package upload

import (
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
// the file grew by append, otherwise the full-file hash. Sources name byte
// ranges of private verified snapshots. full retains the snapshot reference
// for tail fallback; release removes this session's files after its consumer.
type prepared struct {
	root     string
	manifest protocol.Manifest
	bodies   map[string]bodySource
	full     map[string]bodySource
	// cuts is the reader's chunk lengths for a body, by digest, from
	// adapter.Bundle.Cuts.
	cuts    map[string][]int64
	release func() error
}

// prepareEach delivers one scanned and enqueued session at a time. Sync sends
// and ACKs it before the next session is read, so complete transcript bodies
// from unrelated sessions do not all remain live for the duration of a pass.
func prepareEach(ctx context.Context, opt Options, wm *watermark.DB, q *outbox.DB, bundles []adapter.Bundle, consume func(*prepared, *Result) error) (Result, error) {
	var res Result
	var reasons []string
	pending, err := q.Identities(ctx)
	if err != nil {
		return res, err
	}
	for _, bundle := range bundles {
		resPart, err := prepareBundleEach(ctx, opt, wm, q, pending, bundle, consume)
		res.Checked += resPart.Checked
		res.Missing += resPart.Missing
		res.Uploaded += resPart.Uploaded
		res.Manifests += resPart.Manifests
		res.Refused += resPart.Refused
		res.Quarantined += resPart.Quarantined
		res.Unchanged += resPart.Unchanged
		res.Skipped = append(res.Skipped, resPart.Skipped...)
		res.Sessions = append(res.Sessions, resPart.Sessions...)
		res.NoBay = append(res.NoBay, resPart.NoBay...)
		if err != nil {
			if r, ok := err.(*Rejected); ok {
				reasons = append(reasons, r.Reasons...)
				continue
			}
			return res, err
		}
	}
	if len(reasons) == 0 {
		return res, nil
	}
	return res, &Rejected{Reasons: reasons}
}

func prepareBundleEach(ctx context.Context, opt Options, wm *watermark.DB, q *outbox.DB, pending map[string]bool, bundle adapter.Bundle, consume func(*prepared, *Result) error) (Result, error) {
	var res Result
	var reasons []string
	for _, m := range bundle.Manifests {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		if !opt.Projects.Permitted(projectID(m)) {
			res.Refused++
			reasons = append(reasons, allowlistRefusal(m))
			if err := dropPending(ctx, opt, q, bundle.Root, m); err != nil {
				return res, err
			}
			continue
		}
		same, err := unchanged(ctx, opt, wm, pending, bundle.Root, m)
		if err != nil {
			return res, err
		}
		if same {
			res.Unchanged++
			continue
		}
		if hit := knownQuarantine(opt, m); hit != nil {
			res.Quarantined++
			reasons = append(reasons, hit.Error())
			if err := dropPending(ctx, opt, q, bundle.Root, m); err != nil {
				return res, err
			}
			continue
		}
		// Only an admitted session may run git for its root commit.
		m.Project = adapter.ResolveRoot(m.Project)
		item, hit, err := scanSession(ctx, opt, wm, bundle, m)
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
			return res, err
		}
		if hit != nil {
			res.Quarantined++
			reasons = append(reasons, hit.Error())
			if err := dropPending(ctx, opt, q, bundle.Root, m); err != nil {
				return res, err
			}
			continue
		}
		if len(item.manifest.Artifacts) == 0 {
			if err := item.release(); err != nil {
				return res, err
			}
			continue
		}
		err = func() (err error) {
			defer func() { err = errors.Join(err, item.release()) }()
			// Route again on every post, including a retry after no bay.
			item.manifest.Bays = opt.Bays.For(item.manifest.Harness, projectID(item.manifest)).Bays
			item.manifest.BayAware = true
			for d := range item.bodies {
				if c, ok := bundle.Cuts[d]; ok {
					item.cuts[d] = c
				}
			}
			if err := enqueue(ctx, opt, q, item); err != nil {
				return err
			}
			return consume(&item, &res)
		}()
		if err != nil {
			return res, err
		}
	}
	if len(reasons) == 0 {
		return res, nil
	}
	return res, &Rejected{Reasons: reasons}
}

// unchanged reports whether every artifact of m is the file its
// watermark already holds, and the outbox has no row for the session.
// The lake has those bytes and that manifest, so the session is not
// read, scanned, or posted again. The digests come from the reader;
// with a Memo they are the ones taken when the file last had this
// size, mtime, and inode.
func unchanged(ctx context.Context, opt Options, wm *watermark.DB, pending map[string]bool, root string, m protocol.Manifest) (bool, error) {
	if len(m.Artifacts) == 0 || pending[manifestIdentity(opt.MachineID, m.Harness, m.NativeSessionID)] {
		return false, nil
	}
	for _, a := range m.Artifacts {
		if pending[blobIdentity(opt.MachineID, root, a.RelPath)] {
			return false, nil
		}
		mark, ok, err := wm.Get(ctx, markKey(opt, m.Harness, root, a.RelPath))
		if err != nil {
			return false, err
		}
		if !ok || mark.SHA256 != a.SHA256 || mark.Size != a.Size {
			return false, nil
		}
	}
	return true, nil
}

func markKey(opt Options, harness, root, rel string) watermark.Mark {
	return watermark.Mark{MachineID: opt.MachineID, Harness: harness, Root: root, RelPath: rel}
}

// scanHook, when set, hears where each artifact's scan started: 0 for
// the whole file, or the clean prefix it scanned after. Tests set it.
var scanHook func(rel string, clean int64)

// cleanPrefix is how many leading bytes of body an earlier scan found
// clean: the watermarked bytes, when body still starts with them and
// their scan under this ruleset had no hit. Zero means scan it all.
// prefix is the sha256 of body[:mark.Size], or empty when body is
// shorter; stamp uses it for the watermark plan.
func cleanPrefix(ctx context.Context, mark watermark.Mark, ok bool, body bodySource) (clean int64, prefix string, err error) {
	if !ok || mark.Size <= 0 || body.size < mark.Size {
		return 0, "", nil
	}
	prefix, err = body.slice(0, mark.Size).digest(ctx)
	if err != nil {
		return 0, "", err
	}
	if prefix != mark.SHA256 || mark.Ruleset != redact.RulesetV2 || mark.Hits != 0 {
		return 0, prefix, nil
	}
	return mark.Size, prefix, nil
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

// quarantinedDigests is the quarantine log's file records under the
// current ruleset, by digest. A manifest record is left out: its digest
// is of the manifest JSON, not of a file.
func quarantinedDigests(stateDir string) (map[string]redact.Record, error) {
	recs, err := redact.ReadQuarantine(stateDir)
	if err != nil {
		return nil, err
	}
	out := map[string]redact.Record{}
	for _, r := range recs {
		if r.Manifest || r.Ruleset != redact.RulesetV2 || r.SHA256 == "" {
			continue
		}
		out[r.SHA256] = r
	}
	return out, nil
}

// knownQuarantine is the quarantine of a session whose every artifact
// already has a record at its digest, and that no override lets
// through. Those bytes were scanned under this ruleset and held, so
// the session is held again without reading it: a Cursor export is
// rebuilt to be read, and a session that never uploads has no
// watermark to make it unchanged. Nil means read and scan it.
func knownQuarantine(opt Options, m protocol.Manifest) *quarantineHit {
	if opt.UploadHits || len(m.Artifacts) == 0 {
		return nil
	}
	var hit *quarantineHit
	seen := map[string]bool{}
	for _, a := range m.Artifacts {
		rec, ok := opt.quarantined[a.SHA256]
		if a.SHA256 == "" || !ok || opt.allowed[a.SHA256] {
			return nil
		}
		if hit == nil {
			hit = &quarantineHit{rel: a.RelPath}
		}
		hit.hits += rec.Hits
		for _, name := range rec.Rules {
			if !seen[name] {
				seen[name] = true
				hit.rules = append(hit.rules, name)
			}
		}
	}
	return hit
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

// readArtifacts snapshots each artifact from the path its own relpath names
// and checks the bytes against the artifact digest. A mismatch means
// the file changed after hashing, and none of the session is read. A
// read that fails is an *unreadableError.
func readArtifacts(ctx context.Context, bundle adapter.Bundle, m protocol.Manifest, dir string) ([]bodySource, error) {
	out := make([]bodySource, 0, len(m.Artifacts))
	for _, a := range m.Artifacts {
		path := bundle.Paths[a.RelPath]
		if path == "" && bundle.Load != nil {
			p, err := bundle.Load(a.RelPath)
			if err != nil {
				return out, &unreadableError{rel: a.RelPath, err: err}
			}
			path = p
		}
		if path == "" {
			return out, fmt.Errorf("upload: %s: no local path", a.RelPath)
		}
		body, err := snapshotBody(ctx, path, dir, a.Size, a.SHA256)
		if err != nil {
			if errors.Is(err, errFileChanged) || ctx.Err() != nil {
				return out, err
			}
			var pe *os.PathError
			if errors.As(err, &pe) && pe.Path == path {
				return out, &unreadableError{rel: a.RelPath, err: err}
			}
			return out, err
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

func scanSession(ctx context.Context, opt Options, wm *watermark.DB, bundle adapter.Bundle, m protocol.Manifest) (item prepared, blocked *quarantineHit, err error) {
	dir, err := os.MkdirTemp(opt.lakeState(), ".upload-*")
	if err != nil {
		return item, nil, err
	}
	var raws []bodySource
	item.release = func() error {
		var err error
		for _, raw := range raws {
			err = errors.Join(err, raw.file.Close())
		}
		return errors.Join(err, os.RemoveAll(dir))
	}
	defer func() {
		if err != nil || blocked != nil {
			err = errors.Join(err, item.release())
		}
	}()
	raws, err = readArtifacts(ctx, bundle, m, dir)
	if err != nil {
		return item, nil, err
	}
	item.root = bundle.Root
	item.manifest = m
	item.manifest.Artifacts = make([]protocol.Artifact, 0, len(m.Artifacts))
	item.bodies = make(map[string]bodySource, len(m.Artifacts))
	item.full = make(map[string]bodySource, len(m.Artifacts))
	item.cuts = map[string][]int64{}
	seenRules := map[string]bool{}
	for i, a := range m.Artifacts {
		body := raws[i]
		// readArtifacts checked body against a.SHA256.
		digest := a.SHA256
		mark, ok, err := wm.Get(ctx, markKey(opt, m.Harness, bundle.Root, a.RelPath))
		if err != nil {
			return item, nil, err
		}
		clean, prefix, err := cleanPrefix(ctx, mark, ok, body)
		if err != nil {
			return item, nil, err
		}
		if scanHook != nil {
			scanHook(a.RelPath, clean)
		}
		scan, err := (redact.Ruleset{}).ScanReaderAt(ctx, body.file, body.size, clean, dir)
		if err != nil {
			return item, nil, err
		}
		scan = scan.Add(bundle.Hidden[digest])
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
				return item, nil, err
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
		art, blob, hold, err := stamp(ctx, mark, ok, prefix, a, body, scan)
		if err != nil {
			return item, nil, err
		}
		if hold {
			continue
		}
		item.bodies[putDigest(art)] = blob
		item.full[art.RelPath] = body
		item.manifest.Artifacts = append(item.manifest.Artifacts, art)
	}
	if blocked != nil {
		return item, blocked, nil
	}
	if len(item.manifest.Artifacts) > 0 {
		blocked, err = scanManifest(opt, item.manifest)
	}
	return item, blocked, err
}

// stamp plans the upload of body against mark. prefix, when set, is
// the sha256 of body[:mark.Size], already taken for the scan.
func stamp(ctx context.Context, mark watermark.Mark, ok bool, prefix string, a protocol.Artifact, body bodySource, scan redact.Result) (protocol.Artifact, bodySource, bool, error) {
	digest := a.SHA256
	if !ok {
		mark = watermark.Mark{}
	}
	st := watermark.Stat{
		Size:    body.size,
		ModTime: a.MTime,
		FullSHA: digest,
	}
	if ok && body.size > mark.Offset {
		if prefix != "" && mark.Offset == mark.Size {
			st.PrefixSHA = prefix
		} else {
			pfx, err := body.slice(0, mark.Offset).digest(ctx)
			if err != nil {
				return protocol.Artifact{}, bodySource{}, false, err
			}
			st.PrefixSHA = pfx
		}
	}
	dec := watermark.Plan(mark, st)
	// The lake head is ahead of this file and the local bytes still
	// match the snapshot already reported. Another manifest would only
	// repeat that prefix.
	if mark.Offset > mark.Size && dec.Kind == watermark.KindUnchanged && body.size <= mark.Size {
		return protocol.Artifact{}, bodySource{}, true, nil
	}
	status := protocol.RedactionScanned
	if scan.Hits > 0 {
		status = protocol.RedactionOverride
	}
	a.Size = body.size
	a.SHA256 = digest
	a.Redaction = protocol.Redaction{
		Status:  status,
		Ruleset: redact.RulesetV2,
		Hits:    scan.Hits,
	}
	// KindTail is a strict append of the stored prefix. The PUT body
	// is only the suffix. sha256 stays the full file so the lake can
	// check the assembly. Every other kind sends the whole file.
	// The tail is one blob, so it stays under the single-object cap.
	// A file past that cap may still travel as a tail; the upload
	// widens it when the lake does not list protocol.FeatureLargeTails.
	if body.size-dec.Offset <= protocol.MaxBlobBytes && dec.Kind == watermark.KindTail && dec.Offset > 0 && dec.Offset < body.size && int64(dec.Offset+dec.Length) == body.size {
		tail := body.slice(dec.Offset, body.size)
		tailDigest, err := tail.digest(ctx)
		if err != nil {
			return protocol.Artifact{}, bodySource{}, false, err
		}
		a.ByteWatermarkPrev = dec.Offset
		a.TailSHA256 = tailDigest
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
		digest := a.SHA256 // The full immutable snapshot was verified before scanning.
		w.manifest.Artifacts[i].ByteWatermarkPrev = 0
		w.manifest.Artifacts[i].TailSHA256 = digest
		w.manifest.Artifacts[i].SHA256 = digest
		w.manifest.Artifacts[i].Size = body.size
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
