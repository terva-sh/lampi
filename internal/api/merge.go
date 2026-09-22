package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"terva.sh/lampi/internal/cas"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/protocol"
)

// errPrefixMismatch means a tail does not extend the stored head.
// The client sends the whole file and the manifest is decided from that.
var errPrefixMismatch = errors.New("prefix mismatch")

type missingBlobsError struct {
	Missing []string
}

func (e *missingBlobsError) Error() string { return "missing blobs" }

// clientError is a manifest the client got wrong: a size that does not
// match the bytes, or a digest that does not match them.
type clientError struct{ error }

// resolve decides Layer B for each artifact. Equal full digests are a
// no-op. A strict extension of the stored bytes moves the head. A stored
// file that starts with the client bytes keeps the head (stale). Anything
// else is a divergent copy and does not move the head.
func (s *Server) resolve(ctx context.Context, m *protocol.Manifest) ([]catalog.Decision, error) {
	_, current, _, err := s.Catalog.Current(ctx, m.Harness, m.NativeSessionID)
	if err != nil {
		return nil, err
	}
	byRel := map[string]catalog.ArtifactRow{}
	prevBytes := map[string][]byte{}
	for _, row := range current {
		byRel[row.RelPath] = row
		b, err := s.readStored(row.SHA256)
		if err != nil {
			return nil, err
		}
		prevBytes[row.RelPath] = b
	}

	var missing []string
	seen := map[string]bool{}
	addMissing := func(d string) {
		if seen[d] {
			return
		}
		seen[d] = true
		missing = append(missing, d)
	}
	for _, a := range m.Artifacts {
		if a.ByteWatermarkPrev != 0 {
			continue
		}
		ok, err := s.CAS.Has(a.SHA256)
		if err != nil {
			return nil, err
		}
		if !ok {
			addMissing(a.SHA256)
		}
	}
	if len(missing) > 0 {
		return nil, &missingBlobsError{Missing: missing}
	}
	for _, a := range m.Artifacts {
		if a.ByteWatermarkPrev == 0 {
			continue
		}
		prev, hasPrev := byRel[a.RelPath]
		stored := prevBytes[a.RelPath]
		if !hasPrev || int64(len(stored)) != a.ByteWatermarkPrev {
			return nil, errPrefixMismatch
		}
		if a.SHA256 == prev.SHA256 {
			continue
		}
		ok, err := s.CAS.Has(a.TailSHA256)
		if err != nil {
			return nil, err
		}
		if !ok {
			addMissing(a.TailSHA256)
		}
	}
	if len(missing) > 0 {
		return nil, &missingBlobsError{Missing: missing}
	}

	headIdx := headIndex(m.Artifacts)
	decisions := make([]catalog.Decision, len(m.Artifacts))
	for i, a := range m.Artifacts {
		prev, hasPrev := byRel[a.RelPath]
		client, err := s.clientBytes(a, prevBytes[a.RelPath], hasPrev)
		if err != nil {
			return nil, err
		}
		if sha256Hex(client) != a.SHA256 {
			return nil, &clientError{fmt.Errorf("artifact %q sha256 does not match the stored bytes", a.RelPath)}
		}
		if !hasPrev {
			decisions[i] = catalog.Decision{
				Relation: protocol.RelationHead,
				Record:   true,
				Head:     i == headIdx,
			}
			continue
		}
		switch protocol.RelationOf(prevBytes[a.RelPath], client) {
		case protocol.RelationUnchanged:
			decisions[i] = catalog.Decision{Relation: protocol.RelationUnchanged}
		case protocol.RelationGrownFrom:
			decisions[i] = catalog.Decision{
				Relation:  protocol.RelationGrownFrom,
				GrownFrom: prev.SHA256,
				Record:    true,
				Head:      i == headIdx,
			}
		case protocol.RelationStale:
			decisions[i] = catalog.Decision{Relation: protocol.RelationStale}
		default:
			decisions[i] = catalog.Decision{
				Relation: protocol.RelationDivergentCopy,
				Record:   true,
			}
		}
	}
	return decisions, nil
}

func (s *Server) clientBytes(a protocol.Artifact, prev []byte, hasPrev bool) ([]byte, error) {
	if a.ByteWatermarkPrev == 0 {
		return s.readStored(a.SHA256)
	}
	if hasPrev && a.SHA256 == sha256Hex(prev) && int64(len(prev)) == a.Size {
		return prev, nil
	}
	if !hasPrev || int64(len(prev)) != a.ByteWatermarkPrev {
		return nil, errPrefixMismatch
	}
	tail, err := s.readStored(a.TailSHA256)
	if err != nil {
		return nil, err
	}
	if a.Size != int64(len(prev)+len(tail)) {
		return nil, &clientError{fmt.Errorf("artifact %q size %d does not match %d prefix + %d tail", a.RelPath, a.Size, len(prev), len(tail))}
	}
	full := make([]byte, 0, len(prev)+len(tail))
	full = append(full, prev...)
	full = append(full, tail...)
	if sha256Hex(full) != a.SHA256 {
		return nil, errPrefixMismatch
	}
	if _, err := s.CAS.Put(a.SHA256, bytes.NewReader(full), protocol.MaxBlobBytes); err != nil {
		return nil, err
	}
	return full, nil
}

func (s *Server) readStored(digest string) ([]byte, error) {
	ok, err := s.CAS.Has(digest)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("blob %s is not in the store", digest)
	}
	return s.CAS.Read(digest)
}

func headIndex(arts []protocol.Artifact) int {
	for i, a := range arts {
		if a.Kind == protocol.KindTranscriptJSONL {
			return i
		}
	}
	return len(arts) - 1
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func validateManifest(m *protocol.Manifest) error {
	if m.CaptureProtocol != protocol.Version {
		return fmt.Errorf("capture_protocol %d is not supported", m.CaptureProtocol)
	}
	for i, a := range m.Artifacts {
		if len(a.ChunkSHA256s) > 0 {
			return fmt.Errorf("chunked artifacts are not implemented")
		}
		d := strings.ToLower(a.SHA256)
		if !protocol.ValidDigest(d) {
			return fmt.Errorf("invalid artifact digest")
		}
		m.Artifacts[i].SHA256 = d
		if a.ByteWatermarkPrev < 0 || (a.ByteWatermarkPrev > 0 && a.ByteWatermarkPrev >= a.Size) {
			return fmt.Errorf("artifact %q: byte_watermark_prev %d is outside size %d", a.RelPath, a.ByteWatermarkPrev, a.Size)
		}
		if a.ByteWatermarkPrev > 0 {
			tail := strings.ToLower(a.TailSHA256)
			if !protocol.ValidDigest(tail) {
				return fmt.Errorf("artifact %q: invalid tail digest", a.RelPath)
			}
			m.Artifacts[i].TailSHA256 = tail
		}
	}
	return nil
}

func manifestStatus(err error) (int, protocol.ErrorBody) {
	var miss *missingBlobsError
	if errors.As(err, &miss) {
		return http.StatusConflict, protocol.ErrorBody{Error: "missing blobs", Missing: miss.Missing}
	}
	if errors.Is(err, errPrefixMismatch) {
		return http.StatusConflict, protocol.ErrorBody{Error: "prefix mismatch"}
	}
	var client *clientError
	if errors.As(err, &client) || errors.Is(err, cas.ErrRejected) {
		return http.StatusBadRequest, protocol.ErrorBody{Error: err.Error()}
	}
	return http.StatusInternalServerError, protocol.ErrorBody{Error: err.Error()}
}
