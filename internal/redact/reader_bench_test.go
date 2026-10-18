package redact

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
)

// repeatedReaderAt generates a single large record without first allocating
// that record. This keeps fixture setup out of the scanner's memory result.
type repeatedReaderAt struct {
	prefix, suffix string
	repeated       byte
	count          int64
}

func (r repeatedReaderAt) size() int64 {
	return int64(len(r.prefix)) + r.count + int64(len(r.suffix))
}

func (r repeatedReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("negative offset")
	}
	n := 0
	if off < int64(len(r.prefix)) {
		m := copy(p, r.prefix[off:])
		n += m
		off += int64(m)
	}
	end := int64(len(r.prefix)) + r.count
	for n < len(p) && off < end {
		p[n] = r.repeated
		n++
		off++
	}
	if n < len(p) && off < r.size() {
		n += copy(p[n:], r.suffix[off-end:])
	}
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// A normal multi-record transcript does not exercise unbounded match lengths
// or escape runs. Report allocation at two sizes for those cases separately.
func BenchmarkReaderScanLongSpans(b *testing.B) {
	for _, shape := range []string{"key", "escapes"} {
		for _, mib := range []int64{1, 64} {
			b.Run(fmt.Sprintf("%s/%dMiB", shape, mib), func(b *testing.B) {
				raw := repeatedReaderAt{prefix: "aws_secret_access_key=", repeated: 'A', count: mib << 20, suffix: "."}
				if shape == "escapes" {
					raw.prefix = "aws_secret_access_key"
					raw.repeated = '\\'
					raw.suffix = "n=" + strings.Repeat("A", 45) + "."
				}
				dir := b.TempDir()
				b.SetBytes(raw.size())
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					res, err := (Ruleset{}).ScanReaderAt(context.Background(), raw, raw.size(), 0, dir)
					if err != nil || res.Hits != 1 || len(res.Rules) != 1 || res.Rules[0] != "aws-secret-access-key" {
						b.Fatalf("scan=%+v err=%v", res, err)
					}
				}
			})
		}
	}
}
