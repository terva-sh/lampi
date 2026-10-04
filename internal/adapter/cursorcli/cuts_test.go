package cursorcli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
)

func testDoc(t *testing.T, n int, extra ...string) document {
	t.Helper()
	var rows []blobRow
	add := func(text string) {
		data, _, err := presentBlob([]byte(text))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(text))
		rows = append(rows, blobRow{ID: hex.EncodeToString(sum[:]), Data: data})
	}
	for i := range n {
		add(fmt.Sprintf(`{"role":"assistant","content":"turn %d <b>&</b> é %s"}`, i, strings.Repeat("x", i%700)))
	}
	add("\xff\xfe binary")
	for _, e := range extra {
		add(e)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	meta, _, err := presentMeta(hex.EncodeToString([]byte(`{"name":"t","latestRootBlobId":"abc"}`)))
	if err != nil {
		t.Fatal(err)
	}
	return document{
		HarnessVersion: Version, Confidence: Confidence, Source: "acp-sessions/s/store.db", Scope: "session",
		Meta:  []metaRow{{Key: "0", Value: meta}},
		Blobs: rows,
	}
}

func chunkDigests(body []byte, cuts []int64) []string {
	var out []string
	var at int64
	for _, n := range cuts {
		sum := sha256.Sum256(body[at : at+n])
		out = append(out, hex.EncodeToString(sum[:]))
		at += n
	}
	return out
}

// The row-by-row encoding is byte for byte json.Marshal, so an export
// keeps the digest it had before cuts, and the cuts cover it exactly
// in chunks no longer than maxCut.
func TestEncodeDocumentIsMarshal(t *testing.T) {
	for _, n := range []int{0, 1, 300} {
		doc := testDoc(t, n)
		want, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		body, cuts, err := encodeDocument(doc)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != string(want) {
			t.Fatalf("%d rows: encoding differs from json.Marshal", n)
		}
		var total int64
		for _, c := range cuts {
			if c <= 0 || c > maxCut {
				t.Fatalf("chunk of %d", c)
			}
			total += c
		}
		if total != int64(len(body)) {
			t.Fatalf("%d rows: cuts cover %d of %d", n, total, len(body))
		}
	}
}

// A new blob changes the chunk it lands in and the first chunk, which
// holds the meta rows. Every other chunk keeps its digest. The added
// blob's id sorts first, where a fixed-size split shifts every chunk
// after it, so the same two exports split that way share almost
// nothing.
func TestCutsStayPutWhenABlobIsAdded(t *testing.T) {
	before := testDoc(t, 3000)
	early := ""
	for i := 0; early == ""; i++ {
		text := fmt.Sprintf(`{"role":"user","content":"one more %d"}`, i)
		if sum := sha256.Sum256([]byte(text)); sum[0] == 0 && sum[1] < 16 {
			early = text
		}
	}
	after := testDoc(t, 3000, early)
	b1, c1, err := encodeDocument(before)
	if err != nil {
		t.Fatal(err)
	}
	b2, c2, err := encodeDocument(after)
	if err != nil {
		t.Fatal(err)
	}
	if len(c1) < 20 {
		t.Fatalf("only %d chunks; the test needs more", len(c1))
	}
	had := map[string]bool{}
	for _, d := range chunkDigests(b1, c1) {
		had[d] = true
	}
	fresh := 0
	for _, d := range chunkDigests(b2, c2) {
		if !had[d] {
			fresh++
		}
	}
	if fresh > 2 {
		t.Fatalf("%d of %d chunks are new after one added blob", fresh, len(c2))
	}

	fixed := func(n int) []int64 {
		var out []int64
		for left := int64(n); left > 0; left -= 4096 {
			out = append(out, min(left, 4096))
		}
		return out
	}
	had = map[string]bool{}
	for _, d := range chunkDigests(b1, fixed(len(b1))) {
		had[d] = true
	}
	same := 0
	f2 := chunkDigests(b2, fixed(len(b2)))
	for _, d := range f2 {
		if had[d] {
			same++
		}
	}
	if same > len(f2)/2 {
		t.Fatalf("the fixed split kept %d of %d chunks; the comparison proves nothing", same, len(f2))
	}
}

// A row longer than maxCut is cut into pieces no longer than maxCut,
// and the chunk before it closes first.
func TestCutsSplitALongRow(t *testing.T) {
	var c cutter
	c.add(100)
	c.row(maxCut*2+5, "long")
	c.flush()
	var total int64
	for _, n := range c.out {
		if n <= 0 || n > maxCut {
			t.Fatalf("chunk of %d in %v", n, c.out)
		}
		total += n
	}
	if total != maxCut*2+105 || c.out[0] != 100 {
		t.Fatalf("cuts %v", c.out)
	}
}
