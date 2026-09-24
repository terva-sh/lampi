package api

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"terva.sh/lampi/internal/cas"
	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/protocol"
)

// Project reads the session's current transcript and error blobs and
// projects them. raati_json and tasks_json stay in the CAS and are
// not events. Workers call it. The manifest handler does not.
// Export calls it only when the derived JSONL is missing.
// Digests are the catalog head after resolve and ingest.
// A chunk list that fits under the object cap is the assembled blob.
// A longer list is read as its chunks; that concatenation is not a
// blob. A stale or divergent manifest does not replace that head.
// It does not write the CAS. A non-nil error means no derived view;
// callers record it and leave the blobs in place.
//
// terva reads transcript_jsonl and errors_jsonl. claude, codex, and
// opencode read transcript_jsonl only. A codex history.jsonl artifact
// is not a rollout and is not read. An opencode database blob is not
// an export document; the opencode projector records that failure.
// Other harnesses are rejected before a blob is opened.
func (s *Server) Project(ctx context.Context, m protocol.Manifest) ([]normalize.Event, error) {
	if m.Harness != protocol.HarnessTerva && m.Harness != protocol.HarnessClaude && m.Harness != protocol.HarnessCodex && m.Harness != protocol.HarnessOpenCode {
		return nil, fmt.Errorf("normalize: harness %q is not implemented", m.Harness)
	}
	_, arts, ok, err := s.Catalog.Current(ctx, m.Harness, m.NativeSessionID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("normalize: session %s/%s is not in the catalog", m.Harness, m.NativeSessionID)
	}
	var parent string
	if m.Lineage.ParentNativeID != nil {
		parent = *m.Lineage.ParentNativeID
	}
	now := s.now()
	link := protocol.ProjectLinkID(m.Project.GitRemote, m.Project.GitRoot)
	var all []normalize.Event
	for _, a := range arts {
		if !projectKind(m.Harness, a.Kind) || codexHistory(m.Harness, a.RelPath) {
			continue
		}
		raw, err := readBlob(s.CAS, a.SHA256)
		if err != nil {
			return nil, err
		}
		var ev []normalize.Event
		switch m.Harness {
		case protocol.HarnessClaude:
			ev, err = (normalize.Claude{
				Now:            now,
				NativeID:       m.NativeSessionID,
				ParentNativeID: parent,
				HarnessVersion: m.HarnessVersion,
				CWD:            m.Project.CWD,
				GitCommit:      m.Project.GitCommit,
				ProjectID:      link,
				Digest:         a.SHA256,
			}).Normalize(ctx, raw)
		case protocol.HarnessCodex:
			ev, err = (normalize.Codex{
				Now:            now,
				NativeID:       m.NativeSessionID,
				ParentNativeID: parent,
				HarnessVersion: m.HarnessVersion,
				CWD:            m.Project.CWD,
				GitCommit:      m.Project.GitCommit,
				ProjectID:      link,
				Digest:         a.SHA256,
			}).Normalize(ctx, raw)
		case protocol.HarnessOpenCode:
			ev, err = (normalize.OpenCode{
				Now:            now,
				NativeID:       m.NativeSessionID,
				ParentNativeID: parent,
				HarnessVersion: m.HarnessVersion,
				CWD:            m.Project.CWD,
				GitCommit:      m.Project.GitCommit,
				ProjectID:      link,
				Digest:         a.SHA256,
			}).Normalize(ctx, raw)
		default:
			ev, err = (normalize.Terva{
				Now:            now,
				NativeID:       m.NativeSessionID,
				ParentNativeID: parent,
				HarnessVersion: m.HarnessVersion,
				CWD:            m.Project.CWD,
				GitCommit:      m.Project.GitCommit,
				ProjectID:      link,
				Digest:         a.SHA256,
				Kind:           a.Kind,
			}).Normalize(ctx, raw)
		}
		if err != nil {
			return nil, err
		}
		all = append(all, ev...)
	}
	return all, nil
}

// projectKind is the artifact kinds that become events for harness.
// raati_json and tasks_json are terva sidecars and stay out. Claude
// Code, Codex CLI, and OpenCode upload transcript_jsonl only.
func projectKind(harness, kind string) bool {
	switch harness {
	case protocol.HarnessClaude, protocol.HarnessCodex, protocol.HarnessOpenCode:
		return kind == protocol.KindTranscriptJSONL
	default:
		return kind == protocol.KindTranscriptJSONL || kind == protocol.KindErrorsJSONL
	}
}

// codexHistory reports a Codex prompt-history file. It is not a rollout
// and is not projected, including when a manifest names it as
// transcript_jsonl.
func codexHistory(harness, rel string) bool {
	return harness == protocol.HarnessCodex && path.Base(rel) == "history.jsonl"
}

// StoreEvents writes the derived JSONL and the date/harness parquet
// for sessionUID, or records nerr and removes both. The error it
// returns is a failure to record the outcome, not nerr itself. Raw
// blobs are not opened for write.
func (s *Server) StoreEvents(ctx context.Context, sessionUID string, events []normalize.Event, nerr error) error {
	path := filepath.Join(s.Normalized, sessionUID+".jsonl")
	if nerr != nil {
		if err := removeDerived(path, s.Parquet, sessionUID); err != nil {
			return err
		}
		return s.Catalog.SetNormalizeError(ctx, sessionUID, nerr.Error())
	}
	if err := normalize.WriteFile(path, events); err != nil {
		_ = removeDerived(path, s.Parquet, sessionUID)
		if rec := s.Catalog.SetNormalizeError(ctx, sessionUID, err.Error()); rec != nil {
			return rec
		}
		return nil
	}
	if err := normalize.WriteParquet(s.Parquet, sessionUID, events); err != nil {
		_ = removeDerived(path, s.Parquet, sessionUID)
		if rec := s.Catalog.SetNormalizeError(ctx, sessionUID, err.Error()); rec != nil {
			return rec
		}
		return nil
	}
	return s.Catalog.SetNormalizeError(ctx, sessionUID, "")
}

func removeDerived(jsonl, parquetRoot, sessionUID string) error {
	if err := os.Remove(jsonl); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("normalize: %w", err)
	}
	if err := normalize.RemoveParquet(parquetRoot, sessionUID); err != nil {
		return err
	}
	return nil
}

func readBlob(store *cas.Store, digest string) ([]byte, error) {
	// Read follows a logical chunk list. OpenBlob is only the single
	// object, and a file over the cap is not installed as one.
	return store.Read(digest)
}
