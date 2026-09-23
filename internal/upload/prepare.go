package upload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"terva.sh/lampi/internal/adapter/terva"
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
	manifest protocol.Manifest
	bodies   map[string][]byte
	full     map[string][]byte
}

// prepare is the local half of the pipeline: allowlist, ruleset v1,
// watermark plan, outbox. It does not dial the lake.
func prepare(ctx context.Context, opt Options, wm *watermark.DB, q *outbox.DB, bundle terva.Bundle) ([]prepared, Result, error) {
	var res Result
	var reasons []string
	var work []prepared
	for _, m := range bundle.Manifests {
		rel := sessionRel(m)
		id := config.ProjectID{
			CWD:       m.Project.CWD,
			CWDHash:   m.Project.CWDHash,
			GitRemote: m.Project.GitRemote,
		}
		if !opt.Projects.Permitted(id) {
			res.Refused++
			reasons = append(reasons, fmt.Sprintf(
				"%s: not allowlisted for off-box raw (cwd %q, cwd_hash %q, git_remote %q)",
				rel, m.Project.CWD, m.Project.CWDHash, m.Project.GitRemote,
			))
			if err := dropPending(ctx, opt, q, m); err != nil {
				return nil, res, err
			}
			continue
		}
		next, bodies, full, hit, err := scanSession(ctx, opt, wm, bundle, m)
		if err != nil {
			return nil, res, err
		}
		if len(next.Artifacts) == 0 && hit == nil {
			continue
		}
		if hit != nil {
			res.Quarantined++
			reasons = append(reasons, hit.Error())
			if err := dropPending(ctx, opt, q, m); err != nil {
				return nil, res, err
			}
			continue
		}
		item := prepared{manifest: next, bodies: bodies, full: full}
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
	rel   string
	hits  int
	rules []string
}

func (h *quarantineHit) Error() string {
	word := "hits"
	if h.hits == 1 {
		word = "hit"
	}
	return fmt.Sprintf("%s: redaction ruleset v1 found %d %s (%s); quarantined and not uploaded",
		h.rel, h.hits, word, strings.Join(h.rules, ", "))
}

func scanSession(ctx context.Context, opt Options, wm *watermark.DB, bundle terva.Bundle, m protocol.Manifest) (protocol.Manifest, map[string][]byte, map[string][]byte, *quarantineHit, error) {
	next := m
	next.Artifacts = make([]protocol.Artifact, 0, len(m.Artifacts))
	bodies := make(map[string][]byte, len(m.Artifacts))
	full := make(map[string][]byte, len(m.Artifacts))
	var blocked *quarantineHit
	seenRules := map[string]bool{}
	for _, a := range m.Artifacts {
		path := bundle.Paths[a.SHA256]
		if path == "" {
			return protocol.Manifest{}, nil, nil, nil, fmt.Errorf("upload: %s: no local path for %s", a.RelPath, a.SHA256)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return protocol.Manifest{}, nil, nil, nil, err
		}
		scan, err := (redact.Ruleset{}).Scan(body)
		if err != nil {
			return protocol.Manifest{}, nil, nil, nil, err
		}
		if scan.Hits > 0 && !opt.UploadHits {
			sum := sha256.Sum256(body)
			if err := redact.AppendQuarantine(opt.StateDir, redact.Record{
				RelPath: a.RelPath,
				CWD:     m.Project.CWD,
				SHA256:  hex.EncodeToString(sum[:]),
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
		art, blob, hold, err := stamp(ctx, opt, wm, a, body, scan)
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

func stamp(ctx context.Context, opt Options, wm *watermark.DB, a protocol.Artifact, body []byte, scan redact.Result) (protocol.Artifact, []byte, bool, error) {
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	key := watermark.Mark{
		MachineID: opt.MachineID,
		Harness:   protocol.HarnessTerva,
		Root:      opt.TervaHome,
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
		Ruleset: redact.RulesetV1,
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
			Identity: blobIdentity(opt.MachineID, opt.TervaHome, a.RelPath),
			Digest:   putDigest(a),
			Version:  ver,
		}); err != nil {
			return err
		}
	}
	return q.Enqueue(ctx, outbox.Item{
		Identity: manifestIdentity(opt.MachineID, item.manifest.NativeSessionID),
		Digest:   headDigest(item.manifest),
		Manifest: raw,
		Version:  ver,
	})
}

func dropPending(ctx context.Context, opt Options, q *outbox.DB, m protocol.Manifest) error {
	for _, a := range m.Artifacts {
		if err := q.Ack(ctx, outbox.Item{Identity: blobIdentity(opt.MachineID, opt.TervaHome, a.RelPath)}); err != nil {
			return err
		}
	}
	return q.Ack(ctx, outbox.Item{Identity: manifestIdentity(opt.MachineID, m.NativeSessionID)})
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

func manifestIdentity(machine, native string) string {
	return "manifest:" + machine + ":" + native
}
