package api

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"terva.sh/lampi/internal/cas"
	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/protocol"
)

// Project reads the session's current transcript and error blobs and
// projects them. Digests are the catalog head after resolve and ingest,
// so a grown-from or chunk concat is the assembled object already in
// the CAS. A stale or divergent manifest does not replace that head.
// It does not write the CAS. A non-nil error means no derived view;
// callers record it and leave the blobs in place.
func (s *Server) Project(ctx context.Context, m protocol.Manifest) ([]normalize.Event, error) {
	if m.Harness != protocol.HarnessTerva {
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
	var all []normalize.Event
	for _, a := range arts {
		switch a.Kind {
		case protocol.KindTranscriptJSONL, protocol.KindErrorsJSONL:
		default:
			continue
		}
		raw, err := readBlob(s.CAS, a.SHA256)
		if err != nil {
			return nil, err
		}
		ev, err := (normalize.Terva{
			Now:            now,
			NativeID:       m.NativeSessionID,
			ParentNativeID: parent,
			HarnessVersion: m.HarnessVersion,
			CWD:            m.Project.CWD,
			GitCommit:      m.Project.GitCommit,
			Digest:         a.SHA256,
			Kind:           a.Kind,
		}).Normalize(ctx, raw)
		if err != nil {
			return nil, err
		}
		all = append(all, ev...)
	}
	return all, nil
}

// StoreEvents writes the derived JSONL for sessionUID, or records nerr
// and removes that file. The error it returns is a failure to record
// the outcome, not nerr itself. Raw blobs are not opened for write.
func (s *Server) StoreEvents(ctx context.Context, sessionUID string, events []normalize.Event, nerr error) error {
	path := filepath.Join(s.Normalized, sessionUID+".jsonl")
	if nerr != nil {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("normalize: %w", err)
		}
		return s.Catalog.SetNormalizeError(ctx, sessionUID, nerr.Error())
	}
	if err := normalize.WriteFile(path, events); err != nil {
		_ = os.Remove(path)
		if rec := s.Catalog.SetNormalizeError(ctx, sessionUID, err.Error()); rec != nil {
			return rec
		}
		return nil
	}
	return s.Catalog.SetNormalizeError(ctx, sessionUID, "")
}

func readBlob(store *cas.Store, digest string) ([]byte, error) {
	f, err := store.OpenBlob(digest)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}
