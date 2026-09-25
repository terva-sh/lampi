package catalog

import (
	"bytes"
	"strings"
	"testing"

	"terva.sh/lampi/internal/protocol"
)

// Relate reads the two objects as streams and agrees with
// protocol.RelationOf, including at the edges of its buffer.
func TestRelateMatchesRelationOf(t *testing.T) {
	base := []byte(strings.Repeat("0123456789abcdef", relateBuf/16*2+3))
	cases := [][2][]byte{}
	for _, n := range []int{1, relateBuf - 1, relateBuf, relateBuf + 1, 2 * relateBuf, len(base)} {
		cases = append(cases,
			[2][]byte{base[:n], base},                               // grown
			[2][]byte{base, base[:n]},                               // stale
			[2][]byte{base[:n], append(bytes.Clone(base[:n]), 'x')}, // grown by one
		)
		diverged := bytes.Clone(base)
		diverged[n-1] ^= 0xff
		cases = append(cases, [2][]byte{base, diverged})
	}
	for _, c := range cases {
		stored, client := c[0], c[1]
		if bytes.Equal(stored, client) {
			continue
		}
		blobs := memBlobs{digestHex(stored): stored, digestHex(client): client}
		got, err := Relate(blobs, digestHex(stored), digestHex(client))
		if err != nil {
			t.Fatal(err)
		}
		if want := protocol.RelationOf(stored, client); got != want {
			t.Fatalf("stored %d client %d: %s, want %s", len(stored), len(client), got, want)
		}
	}
}

// A client object whose bytes do not hash to its name is an error,
// even when the comparison stopped before its end.
func TestRelateChecksTheClientHash(t *testing.T) {
	stored := []byte("stored bytes")
	client := []byte("other bytes that differ at once")
	blobs := memBlobs{digestHex(stored): stored, digestHex(client): []byte("tampered")}
	if _, err := Relate(blobs, digestHex(stored), digestHex(client)); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("err %v", err)
	}
}
