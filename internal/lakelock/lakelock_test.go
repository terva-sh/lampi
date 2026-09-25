package lakelock

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSecondAcquireIsHeldUntilRelease(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "lake")
	first, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, Name))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(b)) != strconv.Itoa(os.Getpid()) {
		t.Fatalf("lock file says %q", b)
	}

	_, err = Acquire(dir)
	var held *HeldError
	if !errors.Is(err, ErrHeld) || !errors.As(err, &held) || held.PID != os.Getpid() {
		t.Fatalf("second acquire: %v", err)
	}
	if !strings.Contains(err.Error(), "in use by pid") {
		t.Fatalf("message: %v", err)
	}

	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("second release: %v", err)
	}
	again, err := Acquire(dir)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	again.Release()
}
