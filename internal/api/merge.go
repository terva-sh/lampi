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
		if len(a.ChunkSHA256s) > 0 {
			for _, c := range a.ChunkSHA256s {
				ok, err := s.CAS.Has(c)
				if err != nil {
					return nil, err
				}
				if !ok {
					addMissing(c)
				}
			}
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
		// A tail whose ACK was lost is sent again after the commit. The
		// stored head is then the whole file, longer than the watermark.
		if hasPrev && a.SHA256 == prev.SHA256 {
			continue
		}
		if !hasPrev || int64(len(prevBytes[a.RelPath])) != a.ByteWatermarkPrev {
			return nil, errPrefixMismatch
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
	if len(a.ChunkSHA256s) > 0 {
		if err := s.installChunks(a); err != nil {
			return nil, err
		}
	}
	if a.ByteWatermarkPrev == 0 {
		b, err := s.readStored(a.SHA256)
		if err != nil {
			return nil, err
		}
		if int64(len(b)) != a.Size {
			return nil, &clientError{fmt.Errorf("artifact %q size %d does not match stored bytes %d", a.RelPath, a.Size, len(b))}
		}
		return b, nil
	}
	if hasPrev && a.SHA256 == sha256Hex(prev) {
		if int64(len(prev)) != a.Size {
			return nil, &clientError{fmt.Errorf("artifact %q size %d does not match stored bytes %d", a.RelPath, a.Size, len(prev))}
		}
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

// readStored returns the bytes named by digest. A single object is that
// blob. A logical file over the object cap is the concatenation of its
// chunks; that concatenation is not itself a blob, so Has is false.
func (s *Server) readStored(digest string) ([]byte, error) {
	b, err := s.CAS.Read(digest)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// installChunks makes a chunked artifact readable. A concatenation that
// fits under the object cap is installed as one blob. A longer one is
// recorded as a logical file and is not installed. chunk_lengths is
// required for that longer file, and each length has to match the
// stored object.
func (s *Server) installChunks(a protocol.Artifact) error {
	sizes := make([]int64, len(a.ChunkSHA256s))
	var total int64
	for i, c := range a.ChunkSHA256s {
		f, err := s.CAS.OpenBlob(c)
		if err != nil {
			return err
		}
		st, statErr := f.Stat()
		f.Close()
		if statErr != nil {
			return statErr
		}
		if st.Size() <= 0 || st.Size() > protocol.MaxBlobBytes {
			return &clientError{fmt.Errorf("artifact %q: chunk %d is %d bytes; a chunk must be 1..%d", a.RelPath, i, st.Size(), protocol.MaxBlobBytes)}
		}
		if total > (1<<63-1)-st.Size() {
			return &clientError{fmt.Errorf("artifact %q: chunk list overflows", a.RelPath)}
		}
		sizes[i] = st.Size()
		total += st.Size()
	}
	if len(a.ChunkLengths) > 0 {
		if len(a.ChunkLengths) != len(sizes) {
			return &clientError{fmt.Errorf("artifact %q: chunk_lengths does not match chunk_sha256s", a.RelPath)}
		}
		for i, n := range a.ChunkLengths {
			if n != sizes[i] {
				return &clientError{fmt.Errorf("artifact %q: chunk %d is %d bytes, manifest says %d", a.RelPath, i, sizes[i], n)}
			}
		}
	}
	if total != a.Size {
		return &clientError{fmt.Errorf("artifact %q size %d does not match chunk bytes %d", a.RelPath, a.Size, total)}
	}
	if total > protocol.MaxBlobBytes {
		if len(a.ChunkLengths) == 0 {
			return &clientError{fmt.Errorf("artifact %q: chunk_lengths required when the file is larger than %d bytes", a.RelPath, protocol.MaxBlobBytes)}
		}
		_, err := s.CAS.BindLogical(a.SHA256, a.ChunkSHA256s, sizes)
		return err
	}
	_, err := s.CAS.Concat(a.SHA256, a.ChunkSHA256s, protocol.MaxBlobBytes)
	return err
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

// knownHarnesses and knownKinds are the manifest values the lake
// accepts. A new harness or artifact kind is added here with its
// protocol constant.
var (
	knownHarnesses = map[string]bool{
		protocol.HarnessTerva:     true,
		protocol.HarnessClaude:    true,
		protocol.HarnessCodex:     true,
		protocol.HarnessOpenCode:  true,
		protocol.HarnessCursor:    true,
		protocol.HarnessCursorCLI: true,
	}
	knownKinds = map[string]bool{
		protocol.KindTranscriptJSONL:    true,
		protocol.KindErrorsJSONL:        true,
		protocol.KindRaatiJSON:          true,
		protocol.KindTasksJSON:          true,
		protocol.KindCursorStateJSON:    true,
		protocol.KindCursorCLIStoreJSON: true,
	}
)

func validateManifest(m *protocol.Manifest) error {
	if m.CaptureProtocol != protocol.Version {
		return fmt.Errorf("capture_protocol %d is not supported", m.CaptureProtocol)
	}
	if !knownHarnesses[m.Harness] {
		return fmt.Errorf("harness %q is not supported", m.Harness)
	}
	for i, a := range m.Artifacts {
		if !knownKinds[a.Kind] {
			return fmt.Errorf("artifact %q: kind %q is not supported", a.RelPath, a.Kind)
		}
		if a.Size < 0 {
			return fmt.Errorf("artifact %q: size %d is negative", a.RelPath, a.Size)
		}
		if len(a.ChunkSHA256s) > 0 {
			if a.ByteWatermarkPrev != 0 {
				return fmt.Errorf("artifact %q: chunk_sha256s cannot be combined with a tail", a.RelPath)
			}
			parts, err := normalizeDigests(a.ChunkSHA256s)
			if err != nil {
				return fmt.Errorf("artifact %q: %w", a.RelPath, err)
			}
			m.Artifacts[i].ChunkSHA256s = parts
			if len(a.ChunkLengths) > 0 && len(a.ChunkLengths) != len(parts) {
				return fmt.Errorf("artifact %q: chunk_lengths does not match chunk_sha256s", a.RelPath)
			}
			if err := validateChunkLengths(a.RelPath, a.ChunkLengths, a.Size); err != nil {
				return err
			}
		} else if len(a.ChunkLengths) > 0 {
			return fmt.Errorf("artifact %q: chunk_lengths without chunk_sha256s", a.RelPath)
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

func validateChunkLengths(rel string, lengths []int64, size int64) error {
	if len(lengths) == 0 {
		return nil
	}
	var sum int64
	for _, n := range lengths {
		if n <= 0 || n > protocol.MaxBlobBytes {
			return fmt.Errorf("artifact %q: chunk length %d is outside 1..%d", rel, n, protocol.MaxBlobBytes)
		}
		if sum > (1<<63-1)-n {
			return fmt.Errorf("artifact %q: chunk_lengths overflow", rel)
		}
		sum += n
	}
	if sum != size {
		return fmt.Errorf("artifact %q: chunk_lengths sum %d does not match size %d", rel, sum, size)
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
