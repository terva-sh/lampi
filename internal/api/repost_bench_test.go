package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/protocol"
)

// chunkedSession is a terva transcript of about size bytes and the
// manifest that posts it as chunks of at most the blob cap.
func chunkedSession(tb testing.TB, s *Server, size int) protocol.Manifest {
	tb.Helper()
	var b bytes.Buffer
	b.WriteString(`{"type":"meta","meta":{"id":"big","cwd":"/work/app"}}` + "\n")
	// Long lines keep the projection of a large file quick.
	text := strings.Repeat("a long session line ", 400)
	for i := 0; b.Len() < size; i++ {
		fmt.Fprintf(&b, `{"type":"message","message":{"role":"user","content":[{"type":"text","text":"%d %s"}]}}`+"\n", i, text)
	}
	body := b.Bytes()
	var parts []string
	var lengths []int64
	for start := 0; start < len(body); start += int(protocol.MaxBlobBytes) {
		end := min(start+int(protocol.MaxBlobBytes), len(body))
		parts = append(parts, putBlob(tb, s.Handler(), "", body[start:end]))
		lengths = append(lengths, int64(end-start))
	}
	return protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessTerva,
		NativeSessionID: "big",
		Artifacts: []protocol.Artifact{{
			Kind:         protocol.KindTranscriptJSONL,
			RelPath:      "sessions/x/big.jsonl",
			Size:         int64(len(body)),
			SHA256:       shaOf(tb, body),
			ChunkSHA256s: parts,
			ChunkLengths: lengths,
		}},
	}
}

func postManifestOK(tb testing.TB, s *Server, m protocol.Manifest) {
	tb.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/manifests", bytes.NewReader(mustJSON(tb, m)))
	req.Header.Set("Authorization", "Bearer sekret")
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		tb.Fatalf("manifest %d %s", rr.Code, rr.Body)
	}
}

// BenchmarkRepostUnchangedChunked posts a 41 MiB chunked transcript,
// then times and counts the allocations of posting it again unchanged,
// including any normalize work that post starts.
func BenchmarkRepostUnchangedChunked(b *testing.B) {
	s := openServer(b)
	m := chunkedSession(b, s, 41<<20)
	postManifestOK(b, s, m)
	ctx := context.Background()
	if err := s.WaitNormalized(ctx); err != nil {
		b.Fatal(err)
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()
	for range b.N {
		postManifestOK(b, s, m)
		if err := s.WaitNormalized(ctx); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	runtime.ReadMemStats(&after)
	b.ReportMetric(float64(time.Since(start).Milliseconds())/float64(b.N), "ms/repost")
	b.ReportMetric(float64(after.TotalAlloc-before.TotalAlloc)/float64(b.N)/(1<<20), "MiB-alloc/repost")
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
