package redact

import (
	"bytes"
	"math/rand/v2"
	"strings"
	"testing"
)

// transcript is about n bytes of JSONL that looks like a session: text
// with escaped line breaks and quotes, and words that share a prefix
// with a rule (task-, -----, secret_) without being a key.
func transcript(n int) []byte {
	words := strings.Fields(`the a to of and in is it for on with as this that func return err nil if else
		task-list desk-top sk-ip -----\n secret_sauce aws_region github hooks slack AIzaNope xoxo
		package import struct type string int64 byte slice map chan go defer select case
		{"path":"/work/app/main.go"} \"quoted\" \\n\\t fmt.Println(\"hi\") SG.no npm_run hf_x`)
	rng := rand.New(rand.NewPCG(1, 2))
	var b bytes.Buffer
	for b.Len() < n {
		b.WriteString(`{"type":"assistant","message":{"content":[{"type":"text","text":"`)
		for range 200 + rng.IntN(300) {
			b.WriteString(words[rng.IntN(len(words))])
			if rng.IntN(9) == 0 {
				b.WriteString(`\n`)
			} else {
				b.WriteByte(' ')
			}
		}
		b.WriteString("\"}]}}\n")
	}
	return b.Bytes()
}

// plain is about n bytes of lower-case prose with no rule literal in it.
func plain(n int) []byte {
	words := strings.Fields(`the a to of and in is it for on with as this that when where which lake pond`)
	rng := rand.New(rand.NewPCG(3, 4))
	var b bytes.Buffer
	for b.Len() < n {
		b.WriteString(words[rng.IntN(len(words))])
		b.WriteByte(' ')
	}
	return b.Bytes()
}

// BenchmarkScan32MiB is the ruleset over 32 MiB: a transcript, where
// rule literals occur, and prose with none.
func BenchmarkScan32MiB(b *testing.B) {
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"transcript", transcript(32 << 20)},
		{"no-literal", plain(32 << 20)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.SetBytes(int64(len(tc.body)))
			b.ReportAllocs()
			for range b.N {
				res, err := (Ruleset{}).Scan(tc.body)
				if err != nil || res.Hits != 0 {
					b.Fatalf("%+v %v", res, err)
				}
			}
		})
	}
}
