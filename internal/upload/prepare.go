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
// bodies is keyed by the digest the manifest names.
type prepared struct {
	manifest protocol.Manifest
	bodies   map[string][]byte
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
		next, bodies, hit, err := scanSession(ctx, opt, wm, bundle, m)
		if err != nil {
			return nil, res, err
		}
		if hit != nil {
			res.Quarantined++
			reasons = append(reasons, hit.Error())
			if err := dropPending(ctx, opt, q, m); err != nil {
				return nil, res, err
			}
			continue
		}
		item := prepared{manifest: next, bodies: bodies}
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

func scanSession(ctx context.Context, opt Options, wm *watermark.DB, bundle terva.Bundle, m protocol.Manifest) (protocol.Manifest, map[string][]byte, *quarantineHit, error) {
	next := m
	next.Artifacts = make([]protocol.Artifact, 0, len(m.Artifacts))
	bodies := make(map[string][]byte, len(m.Artifacts))
	var blocked *quarantineHit
	seenRules := map[string]bool{}
	for _, a := range m.Artifacts {
		path := bundle.Paths[a.SHA256]
		if path == "" {
			return protocol.Manifest{}, nil, nil, fmt.Errorf("upload: %s: no local path for %s", a.RelPath, a.SHA256)
		}
		info, err := os.Stat(path)
		if err != nil {
			return protocol.Manifest{}, nil, nil, err
		}
		if info.Size() > protocol.MaxBlobBytes {
			return protocol.Manifest{}, nil, nil, fmt.Errorf("upload: %s is %d bytes; chunked upload is not implemented (max %d)", a.RelPath, info.Size(), protocol.MaxBlobBytes)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return protocol.Manifest{}, nil, nil, err
		}
		if int64(len(body)) > protocol.MaxBlobBytes {
			return protocol.Manifest{}, nil, nil, fmt.Errorf("upload: %s is %d bytes; chunked upload is not implemented (max %d)", a.RelPath, len(body), protocol.MaxBlobBytes)
		}
		scan, err := (redact.Ruleset{}).Scan(body)
		if err != nil {
			return protocol.Manifest{}, nil, nil, err
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
				return protocol.Manifest{}, nil, nil, err
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
		art, err := stamp(ctx, opt, wm, a, body, scan)
		if err != nil {
			return protocol.Manifest{}, nil, nil, err
		}
		bodies[art.SHA256] = body
		next.Artifacts = append(next.Artifacts, art)
	}
	if blocked != nil {
		return protocol.Manifest{}, nil, blocked, nil
	}
	return next, bodies, nil, nil
}

func stamp(ctx context.Context, opt Options, wm *watermark.DB, a protocol.Artifact, body []byte, scan redact.Result) (protocol.Artifact, error) {
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
		return protocol.Artifact{}, err
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
			return protocol.Artifact{}, err
		}
		st.PrefixSHA = pfx
	}
	dec := watermark.Plan(mark, st)
	// Plan is the local cursor. KindUnchanged is why the digest check
	// uploads nothing. KindTail means the file grew by append, but the
	// body in the map below is still the whole file. The manifest has
	// to describe that blob: prev 0 and tail_sha256 equal to sha256.
	// A non-zero prev means the blob is only the bytes after that
	// offset, which would duplicate the prefix if a later merge
	// concatenated them.
	prev, tail := fullFileFields(dec, digest)
	status := protocol.RedactionScanned
	if scan.Hits > 0 {
		status = protocol.RedactionOverride
	}
	a.Size = int64(len(body))
	a.SHA256 = digest
	a.ByteWatermarkPrev = prev
	a.TailSHA256 = tail
	a.Redaction = protocol.Redaction{
		Status:  status,
		Ruleset: redact.RulesetV1,
		Hits:    scan.Hits,
	}
	return a, nil
}

// fullFileFields is the wire watermark for a PUT of the entire file.
// Every Plan kind takes this path today, including KindTail and
// KindUnchanged. Non-zero prev and a tail hash other than digest are
// reserved for a PUT of the suffix Plan named.
func fullFileFields(dec watermark.Decision, digest string) (int64, string) {
	switch dec.Kind {
	case watermark.KindNew, watermark.KindUnchanged, watermark.KindTail, watermark.KindReplace, watermark.KindProbe:
		return 0, digest
	default:
		return 0, digest
	}
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
			Digest:   a.SHA256,
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
