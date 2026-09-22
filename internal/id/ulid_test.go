package id

import (
	"testing"
	"time"
)

func TestNewShape(t *testing.T) {
	a, err := New(time.Date(2026, 9, 22, 16, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(time.Date(2026, 9, 22, 16, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 26 || len(b) != 26 {
		t.Fatalf("length %d %d", len(a), len(b))
	}
	if a == b {
		t.Fatal("two ids minted in the same millisecond collided")
	}
	for _, id := range []string{a, b} {
		for _, c := range id {
			if !valid(c) {
				t.Fatalf("%q has %q", id, c)
			}
		}
	}
	// Same millisecond: the time prefix (first 10 chars) matches.
	if a[:10] != b[:10] {
		t.Fatalf("time prefix %s vs %s", a[:10], b[:10])
	}
}

func valid(c rune) bool {
	switch {
	case c >= '0' && c <= '9':
		return true
	case c >= 'A' && c <= 'H':
		return true
	case c >= 'J' && c <= 'N':
		return true
	case c >= 'P' && c <= 'T':
		return true
	case c >= 'V' && c <= 'Z':
		return true
	default:
		return false
	}
}
