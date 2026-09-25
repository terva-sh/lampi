package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"terva.sh/lampi/internal/cas"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/protocol"
)

// Project reads the session head and the blobs that belong with it and
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
// The head is one artifact. For terva, claude, and codex, the current
// artifacts at or under the head's directory are read too: a terva
// error sidecar and a Claude subagent transcript sit there. The same
// session posted under another relpath is not read a second time.
// cursor, cursor-cli, and opencode read the head only, because that
// export is the whole session.
//
// terva reads transcript_jsonl and errors_jsonl. claude and codex read
// transcript_jsonl only. A codex history.jsonl artifact is not a
// rollout and is not read. opencode reads opencode_export_json, and
// transcript_jsonl from an agent older than that kind. An opencode
// database blob is not an export document; the opencode projector
// records that failure. cursor reads cursor_state_json only.
// cursor-cli reads cursor_cli_store_json only. A cursor, cursor-cli,
// or opencode session whose head is not such an artifact is an error,
// not an empty success. Other harnesses are rejected before a blob is
// opened.
func (s *Server) Project(ctx context.Context, m protocol.Manifest) ([]normalize.Event, error) {
	if m.Harness != protocol.HarnessTerva && m.Harness != protocol.HarnessClaude && m.Harness != protocol.HarnessCodex && m.Harness != protocol.HarnessOpenCode && m.Harness != protocol.HarnessCursor && m.Harness != protocol.HarnessCursorCLI {
		return nil, fmt.Errorf("normalize: harness %q is not implemented", m.Harness)
	}
	view, ok, err := s.Catalog.Head(ctx, m.Harness, m.NativeSessionID)
	if err != nil {
		return nil, transientError{err}
	}
	if !ok {
		return nil, fmt.Errorf("normalize: session %s/%s is not in the catalog", m.Harness, m.NativeSessionID)
	}
	arts := headArtifacts(m.Harness, view)
	var parent string
	if m.Lineage.ParentNativeID != nil {
		parent = *m.Lineage.ParentNativeID
	}
	now := s.now()
	link := protocol.ProjectLinkID(m.Project.GitRemote, m.Project.GitRoot)
	var all []normalize.Event
	var projected bool
	for _, a := range arts {
		if !projectKind(m.Harness, a.Kind) || codexHistory(m.Harness, a.RelPath) {
			continue
		}
		projected = true
		raw, err := readBlob(s.CAS, a.SHA256)
		if err != nil {
			return nil, transientError{err}
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
		case protocol.HarnessCursor:
			ev, err = (normalize.Cursor{
				Now:            now,
				NativeID:       m.NativeSessionID,
				ParentNativeID: parent,
				HarnessVersion: m.HarnessVersion,
				CWD:            m.Project.CWD,
				GitCommit:      m.Project.GitCommit,
				ProjectID:      link,
				Digest:         a.SHA256,
			}).Normalize(ctx, raw)
		case protocol.HarnessCursorCLI:
			ev, err = (normalize.CursorCLI{
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
	// Skipping every artifact and publishing nothing is a success for
	// an empty terva sidecar set. For cursor it means the head is not
	// cursor_state_json. For cursor-cli it means the head is not
	// cursor_cli_store_json. For opencode it means the head is not an
	// export. An empty event list would clear normalize_error. That is
	// a failure.
	if m.Harness == protocol.HarnessCursor && !projected {
		return nil, fmt.Errorf("normalize: cursor session has no cursor_state_json artifact")
	}
	if m.Harness == protocol.HarnessCursorCLI && !projected {
		return nil, fmt.Errorf("normalize: cursor-cli session has no cursor_cli_store_json artifact")
	}
	if m.Harness == protocol.HarnessOpenCode && !projected {
		return nil, fmt.Errorf("normalize: opencode session has no opencode_export_json artifact")
	}
	return all, nil
}

// transientError is a Project failure of the lake, not of the bytes: a
// catalog read or a CAS read. The worker tries it again.
type transientError struct{ err error }

func (e transientError) Error() string { return e.err.Error() }
func (e transientError) Unwrap() error { return e.err }

func isTransient(err error) bool {
	var t transientError
	return errors.As(err, &t)
}

// headArtifacts is the current artifacts Project reads, in relpath
// order. The head row is always one of them. cursor, cursor-cli, and
// opencode keep the head only. terva, claude, and codex also keep the
// rows at or under the head's directory. A session whose head is not
// a current row keeps every current row, as before the head was
// tracked per session.
func headArtifacts(harness string, v catalog.HeadView) []catalog.ArtifactRow {
	head := -1
	for i, a := range v.Current {
		if a.SHA256 == v.HeadSHA256 {
			head = i
			break
		}
	}
	if head < 0 {
		return v.Current
	}
	switch harness {
	case protocol.HarnessCursor, protocol.HarnessCursorCLI, protocol.HarnessOpenCode:
		return v.Current[head : head+1]
	}
	dir := path.Dir(v.Current[head].RelPath)
	out := make([]catalog.ArtifactRow, 0, len(v.Current))
	for i, a := range v.Current {
		// A head at the top level takes only its top-level peers, so a
		// leftover current row under some other directory stays out.
		peer := strings.HasPrefix(a.RelPath, dir+"/")
		if dir == "." {
			peer = path.Dir(a.RelPath) == "."
		}
		if i == head || peer {
			out = append(out, a)
		}
	}
	return out
}

// projectKind is the artifact kinds that become events for harness.
// raati_json and tasks_json are terva sidecars and stay out. Claude
// Code and Codex CLI upload transcript_jsonl only. OpenCode uploads
// opencode_export_json; an older agent labelled the same document
// transcript_jsonl. Cursor IDE uploads cursor_state_json. Cursor CLI
// uploads cursor_cli_store_json. The two Cursor kinds are not
// interchangeable.
func projectKind(harness, kind string) bool {
	switch harness {
	case protocol.HarnessClaude, protocol.HarnessCodex:
		return kind == protocol.KindTranscriptJSONL
	case protocol.HarnessOpenCode:
		return kind == protocol.KindOpenCodeExportJSON || kind == protocol.KindTranscriptJSONL
	case protocol.HarnessCursor:
		return kind == protocol.KindCursorStateJSON
	case protocol.HarnessCursorCLI:
		return kind == protocol.KindCursorCLIStoreJSON
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
