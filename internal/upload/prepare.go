package upload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/outbox"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/redact"
	"terva.sh/lampi/internal/watermark"
)

// prepared is one session that passed the allowlist and the scan.
// bodies is keyed by the digest that will be PUT: the tail hash when
// the file grew by append, otherwise the full-file hash. full keeps
// the whole file so a tail the lake rejects can be sent as one blob.
type prepared struct {
	root     string
	manifest protocol.Manifest
	bodies   map[string][]byte
	full     map[string][]byte
}

// prepare is the local half of the pipeline: allowlist, ruleset v2,
// watermark plan, outbox. It does not dial the lake.
func prepare(ctx context.Context, opt Options, wm *watermark.DB, q *outbox.DB, bundles []adapter.Bundle) ([]prepared, Result, error) {
	var res Result
	var reasons []string
	var work []prepared
	for _, bundle := range bundles {
		part, resPart, err := prepareBundle(ctx, opt, wm, q, bundle)
		res.Checked += resPart.Checked
		res.Refused += resPart.Refused
		res.Quarantined += resPart.Quarantined
		res.Skipped = append(res.Skipped, resPart.Skipped...)
		work = append(work, part...)
		if err != nil {
			if r, ok := err.(*Rejected); ok {
				reasons = append(reasons, r.Reasons...)
				continue
			}
			return nil, res, err
		}
	}
	if len(reasons) == 0 {
		return work, res, nil
	}
	return work, res, &Rejected{Reasons: reasons}
}

func prepareBundle(ctx context.Context, opt Options, wm *watermark.DB, q *outbox.DB, bundle adapter.Bundle) ([]prepared, Result, error) {
	var res Result
	var reasons []string
	var work []prepared
	for _, m := range bundle.Manifests {
		if !opt.Projects.Permitted(projectID(m)) {
			res.Refused++
			reasons = append(reasons, allowlistRefusal(m))
			if err := dropPending(ctx, opt, q, bundle.Root, m); err != nil {
				return nil, res, err
			}
			continue
		}
		// Only an admitted session may run git for its root commit.
		m.Project = adapter.ResolveRoot(m.Project)
		next, bodies, full, hit, err := scanSession(ctx, opt, wm, bundle, m)
		if errors.Is(err, errFileChanged) {
			// The next write event syncs it again with a fresh digest.
			continue
		}
		var unread *unreadableError
		if errors.As(err, &unread) {
			// Gone or unreadable since the digest was taken. The other
			// sessions still upload, and the next pass tries this one.
			res.Skipped = append(res.Skipped, unread.Error())
			continue
		}
		if err != nil {
			return nil, res, err
		}
		if len(next.Artifacts) == 0 && hit == nil {
			continue
		}
		if hit == nil {
			if hit, err = scanManifest(opt, next); err != nil {
				return nil, res, err
			}
		}
		if hit != nil {
			res.Quarantined++
			reasons = append(reasons, hit.Error())
			if err := dropPending(ctx, opt, q, bundle.Root, m); err != nil {
				return nil, res, err
			}
			continue
		}
		item := prepared{root: bundle.Root, manifest: next, bodies: bodies, full: full}
		if err := enqueue(ctx, opt, q, item); err != nil {
			return nil, res, err
		}
		work = append(work, item)
	}
	if len(reasons) == 0 {
		return work, res, nil
	}
	return work, res, &Rejected{Reasons: reasons}
}

type quarantineHit struct {
	rel      string
	hits     int
	rules    []string
	manifest bool
}

func (h *quarantineHit) Error() string {
	word := "hits"
	if h.hits == 1 {
		word = "hit"
	}
	where := ""
	if h.manifest {
		where = " in the manifest"
	}
	return fmt.Sprintf("%s: redaction ruleset %s found %d %s%s (%s); quarantined and not uploaded",
		h.rel, redact.RulesetV2, h.hits, word, where, strings.Join(h.rules, ", "))
}

// errFileChanged is a session whose file no longer hashes to the digest
// its manifest was built with. It is skipped for this round.
var errFileChanged = errors.New("upload: file changed since its digest was taken")

// unreadableError is an artifact that could not be read after its
// digest was taken: removed, or no longer readable. Its session is left
// out of this pass.
type unreadableError struct {
	rel string
	err error
}

func (e *unreadableError) Error() string {
	return fmt.Sprintf("%s: %v", e.rel, e.err)
}

func (e *unreadableError) Unwrap() error { return e.err }

// readArtifacts reads each artifact from the path its own relpath names
// and checks the bytes against the artifact digest. A mismatch means
// the file changed after hashing, and none of the session is read. A
// read that fails is an *unreadableError.
func readArtifacts(bundle adapter.Bundle, m protocol.Manifest) ([][]byte, error) {
	out := make([][]byte, 0, len(m.Artifacts))
	for _, a := range m.Artifacts {
		path := bundle.Paths[a.RelPath]
		if path == "" {
			return nil, fmt.Errorf("upload: %s: no local path", a.RelPath)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, &unreadableError{rel: a.RelPath, err: err}
		}
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != a.SHA256 {
			return nil, errFileChanged
		}
		out = append(out, body)
	}
	return out, nil
}

// scanManifest runs the ruleset over the manifest JSON that would be
// posted. The cwd, the remote, and the relpaths are on it. A hit is
// refused whatever redaction.upload_hits says, since the manifest has
// no redaction stamp to carry an override. The match can sit in any of
// those fields, so the quarantine record and the refusal line carry
// the relpath with matches stripped, the rules, and the manifest
// digest, and not the cwd.
func scanManifest(opt Options, m protocol.Manifest) (*quarantineHit, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	scan, err := (redact.Ruleset{}).Scan(raw)
	if err != nil {
		return nil, err
	}
	if scan.Hits == 0 {
		return nil, nil
	}
	sum := sha256.Sum256(raw)
	rel := (redact.Ruleset{}).Strip(sessionRel(m))
	if err := redact.AppendQuarantine(opt.StateDir, redact.Record{
		RelPath:  rel,
		SHA256:   hex.EncodeToString(sum[:]),
		Ruleset:  scan.Ruleset,
		Hits:     scan.Hits,
		Rules:    scan.Rules,
		Manifest: true,
	}); err != nil {
		return nil, err
	}
	return &quarantineHit{rel: rel, hits: scan.Hits, rules: scan.Rules, manifest: true}, nil
}

func scanSession(ctx context.Context, opt Options, wm *watermark.DB, bundle adapter.Bundle, m protocol.Manifest) (protocol.Manifest, map[string][]byte, map[string][]byte, *quarantineHit, error) {
	raws, err := readArtifacts(bundle, m)
	if err != nil {
		return protocol.Manifest{}, nil, nil, nil, err
	}
	next := m
	next.Artifacts = make([]protocol.Artifact, 0, len(m.Artifacts))
	bodies := make(map[string][]byte, len(m.Artifacts))
	full := make(map[string][]byte, len(m.Artifacts))
	var blocked *quarantineHit
	seenRules := map[string]bool{}
	for i, a := range m.Artifacts {
		body := raws[i]
		scan, err := (redact.Ruleset{}).Scan(body)
		if err != nil {
			return protocol.Manifest{}, nil, nil, nil, err
		}
		scan = scan.Add(bundle.Hidden[a.SHA256])
		sum := sha256.Sum256(body)
		digest := hex.EncodeToString(sum[:])
		// quarantine allow acknowledges these exact bytes. Anything
		// else, including the same file after it grows, is held.
		if scan.Hits > 0 && !opt.UploadHits && !opt.allowed[digest] {
			if err := redact.AppendQuarantine(opt.StateDir, redact.Record{
				RelPath: a.RelPath,
				CWD:     m.Project.CWD,
				SHA256:  digest,
				Ruleset: scan.Ruleset,
				Hits:    scan.Hits,
				Rules:   scan.Rules,
			}); err != nil {
				return protocol.Manifest{}, nil, nil, nil, err
			}
			if blocked == nil {
				blocked = &quarantineHit{rel: a.RelPath}
			}
			blocked.hits += scan.Hits
			for _, name := range scan.Rules {
				if seenRules[name] {
					continue
				}
				seenRules[name] = true
				blocked.rules = append(blocked.rules, name)
			}
			continue
		}
		art, blob, hold, err := stamp(ctx, opt, wm, bundle.Root, m.Harness, a, body, scan)
		if err != nil {
			return protocol.Manifest{}, nil, nil, nil, err
		}
		if hold {
			continue
		}
		bodies[putDigest(art)] = blob
		full[art.RelPath] = body
		next.Artifacts = append(next.Artifacts, art)
	}
	if blocked != nil {
		return protocol.Manifest{}, nil, nil, blocked, nil
	}
	return next, bodies, full, nil, nil
}

func stamp(ctx context.Context, opt Options, wm *watermark.DB, root, harness string, a protocol.Artifact, body []byte, scan redact.Result) (protocol.Artifact, []byte, bool, error) {
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	key := watermark.Mark{
		MachineID: opt.MachineID,
		Harness:   harness,
		Root:      root,
		RelPath:   a.RelPath,
	}
	mark, ok, err := wm.Get(ctx, key)
	if err != nil {
		return protocol.Artifact{}, nil, false, err
	}
	if !ok {
		mark = key
	}
	st := watermark.Stat{
		Size:    int64(len(body)),
		ModTime: a.MTime,
		FullSHA: digest,
	}
	if ok && int64(len(body)) > mark.Offset {
		pfx, err := watermark.HashPrefix(bytes.NewReader(body), mark.Offset)
		if err != nil {
			return protocol.Artifact{}, nil, false, err
		}
		st.PrefixSHA = pfx
	}
	dec := watermark.Plan(mark, st)
	// The lake head is ahead of this file and the local bytes still
	// match the snapshot already reported. Another manifest would only
	// repeat that prefix.
	if mark.Offset > mark.Size && dec.Kind == watermark.KindUnchanged && int64(len(body)) <= mark.Size {
		return protocol.Artifact{}, nil, true, nil
	}
	status := protocol.RedactionScanned
	if scan.Hits > 0 {
		status = protocol.RedactionOverride
	}
	a.Size = int64(len(body))
	a.SHA256 = digest
	a.Redaction = protocol.Redaction{
		Status:  status,
		Ruleset: redact.RulesetV2,
		Hits:    scan.Hits,
	}
	// KindTail is a strict append of the stored prefix. The PUT body
	// is only the suffix. sha256 stays the full file so the lake can
	// check the assembly. Every other kind sends the whole file.
	// A file past the single-object cap is sent whole. A tail cannot
	// carry a chunk list, and assembling one onto the stored head
	// would install an object past the cap. The upload splits it.
	if int64(len(body)) <= protocol.MaxBlobBytes && dec.Kind == watermark.KindTail && dec.Offset > 0 && dec.Offset < int64(len(body)) && int64(dec.Offset+dec.Length) == int64(len(body)) {
		tail := body[dec.Offset:]
		sum := sha256.Sum256(tail)
		a.ByteWatermarkPrev = dec.Offset
		a.TailSHA256 = hex.EncodeToString(sum[:])
		return a, tail, false, nil
	}
	a.ByteWatermarkPrev = 0
	a.TailSHA256 = digest
	return a, body, false, nil
}

// widenToFullFile rewrites a tail manifest into a whole-file manifest.
// It reports whether any artifact changed. The lake asks for this when
// the suffix does not extend the stored head.
func widenToFullFile(w *prepared) bool {
	changed := false
	for i, a := range w.manifest.Artifacts {
		if a.ByteWatermarkPrev == 0 {
			continue
		}
		body, ok := w.full[a.RelPath]
		if !ok {
			continue
		}
		sum := sha256.Sum256(body)
		digest := hex.EncodeToString(sum[:])
		w.manifest.Artifacts[i].ByteWatermarkPrev = 0
		w.manifest.Artifacts[i].TailSHA256 = digest
		w.manifest.Artifacts[i].SHA256 = digest
		w.manifest.Artifacts[i].Size = int64(len(body))
		w.bodies[digest] = body
		changed = true
	}
	return changed
}

func enqueue(ctx context.Context, opt Options, q *outbox.DB, item prepared) error {
	ver := nextVersion()
	raw, err := json.Marshal(item.manifest)
	if err != nil {
		return err
	}
	for _, a := range item.manifest.Artifacts {
		if err := q.Enqueue(ctx, outbox.Item{
			Identity: blobIdentity(opt.MachineID, item.root, a.RelPath),
			Digest:   putDigest(a),
			Version:  ver,
		}); err != nil {
			return err
		}
	}
	return q.Enqueue(ctx, outbox.Item{
		Identity: manifestIdentity(opt.MachineID, item.manifest.Harness, item.manifest.NativeSessionID),
		Digest:   headDigest(item.manifest),
		Manifest: raw,
		Version:  ver,
	})
}

func dropPending(ctx context.Context, opt Options, q *outbox.DB, root string, m protocol.Manifest) error {
	for _, a := range m.Artifacts {
		if err := q.Ack(ctx, outbox.Item{Identity: blobIdentity(opt.MachineID, root, a.RelPath)}); err != nil {
			return err
		}
	}
	return q.Ack(ctx, outbox.Item{Identity: manifestIdentity(opt.MachineID, m.Harness, m.NativeSessionID)})
}

// allowlistRefusal is the stderr line for a session the existing
// permit check kept on the machine. The check itself is unchanged.
// An empty cwd on the Cursor IDE or the Cursor CLI adds why that
// export has no project path.
func allowlistRefusal(m protocol.Manifest) string {
	msg := fmt.Sprintf(
		"%s: not allowlisted for off-box raw (cwd %q, cwd_hash %q, git_remote %q)",
		sessionRel(m), m.Project.CWD, m.Project.CWDHash, m.Project.GitRemote,
	)
	if hint := cursorEmptyCWDHint(m); hint != "" {
		msg += ". " + hint
	}
	return msg
}

// cursorEmptyCWDHint explains an empty cwd the Cursor readers store
// on purpose. Other harnesses, and a Cursor session that has a cwd,
// get no extra text. Matching still uses Projects.Permitted.
func cursorEmptyCWDHint(m protocol.Manifest) string {
	if m.Project.CWD != "" {
		return ""
	}
	switch m.Harness {
	case protocol.HarnessCursor:
		if m.NativeSessionID == "global" {
			return "Cursor IDE global database has an empty cwd and is refused by design; a workspace database takes its cwd from workspace.json"
		}
		return "Cursor IDE workspace has an empty cwd; its cwd comes from workspace.json, and a URI with no local path is refused"
	case protocol.HarnessCursorCLI:
		return "Cursor CLI export needs an absolute cwd in the sibling meta.json; without one the allowlist refuses it"
	default:
		return ""
	}
}

func sessionRel(m protocol.Manifest) string {
	for _, a := range m.Artifacts {
		if a.Kind == protocol.KindTranscriptJSONL {
			return a.RelPath
		}
	}
	if len(m.Artifacts) > 0 {
		return m.Artifacts[0].RelPath
	}
	return m.NativeSessionID
}

func headDigest(m protocol.Manifest) string {
	for _, a := range m.Artifacts {
		if a.Kind == protocol.KindTranscriptJSONL {
			return a.SHA256
		}
	}
	if len(m.Artifacts) == 0 {
		return ""
	}
	return m.Artifacts[len(m.Artifacts)-1].SHA256
}

func blobIdentity(machine, root, rel string) string {
	return "blob:" + machine + ":" + root + ":" + rel
}

func manifestIdentity(machine, harness, native string) string {
	return "manifest:" + machine + ":" + harness + ":" + native
}

// projectID is what the allowlist sees for m. NoRepo is set only when
// the cwd is known to sit outside any checkout, so a git_remote deny
// still refuses a session whose remote could not be read.
func projectID(m protocol.Manifest) config.ProjectID {
	return config.ProjectID{
		CWD:       m.Project.CWD,
		CWDHash:   m.Project.CWDHash,
		GitRemote: m.Project.GitRemote,
		NoRepo:    m.Project.GitRemote == "" && adapter.OutsideCheckout(m.Project.CWD),
	}
}
