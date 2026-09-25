package adapter

import (
	"bytes"
	"strings"
	"testing"
)

func TestScanLinesSkipsALongLine(t *testing.T) {
	long := strings.Repeat("x", 200*1024)
	in := "a\r\n" + long + "\nb\n\nc"
	var got []string
	longs := 0
	err := ScanLines(strings.NewReader(in), 1024, func(line []byte, over bool) bool {
		if over {
			if line != nil {
				t.Fatal("a long line was handed over")
			}
			longs++
			return true
		}
		got = append(got, string(line))
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if longs != 1 || strings.Join(got, "|") != "a|b||c" {
		t.Fatalf("longs %d lines %q", longs, got)
	}
}

func TestScanLinesStops(t *testing.T) {
	n := 0
	err := ScanLines(bytes.NewReader([]byte("1\n2\n3\n")), 16, func([]byte, bool) bool {
		n++
		return n < 2
	})
	if err != nil || n != 2 {
		t.Fatalf("n %d err %v", n, err)
	}
}

func TestScanLinesExactCap(t *testing.T) {
	in := strings.Repeat("y", 64) + "\n" + strings.Repeat("z", 65) + "\n"
	var lens []int
	longs := 0
	if err := ScanLines(strings.NewReader(in), 64, func(line []byte, over bool) bool {
		if over {
			longs++
		} else {
			lens = append(lens, len(line))
		}
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if len(lens) != 1 || lens[0] != 64 || longs != 1 {
		t.Fatalf("lens %v longs %d", lens, longs)
	}
}
