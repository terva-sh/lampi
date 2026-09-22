package terva

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"terva.sh/lampi/internal/protocol"
)

func TestManifestsGroupSidecar(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "abcd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	transcript := "{\"type\":\"meta\",\"meta\":{\"id\":\"20260922-161000-abcd1234\",\"cwd\":\"/home/drew/src/foo\",\"parent\":\"parent-1\",\"fork_point\":4}}\n{\"type\":\"message\"}\n"
	if err := os.WriteFile(filepath.Join(dir, "20260922-161000-abcd1234.jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "20260922-161000-abcd1234.errors.jsonl"), []byte("{\"type\":\"error\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(home, "sessions", "eeee")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "solo.jsonl"), []byte("not-json\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	b, err := Manifests(home, "machine-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Manifests) != 2 {
		t.Fatalf("manifests: %d", len(b.Manifests))
	}
	var grouped protocol.Manifest
	for _, m := range b.Manifests {
		if m.NativeSessionID == "20260922-161000-abcd1234" {
			grouped = m
		}
		if m.MachineID != "machine-1" || m.Harness != protocol.HarnessTerva || m.CaptureProtocol != 1 {
			t.Fatalf("header: %+v", m)
		}
		for _, a := range m.Artifacts {
			if a.Redaction.Status != protocol.RedactionUnscanned {
				t.Fatalf("redaction %s", a.Redaction.Status)
			}
			if a.ChunkSHA256s != nil || a.ByteWatermarkPrev != 0 || a.TailSHA256 != a.SHA256 {
				t.Fatalf("artifact watermark: %+v", a)
			}
			if b.Paths[a.SHA256] == "" {
				t.Fatalf("missing path for %s", a.SHA256)
			}
		}
	}
	if len(grouped.Artifacts) != 2 {
		t.Fatalf("grouped artifacts: %+v", grouped.Artifacts)
	}
	if grouped.Project.CWD != "/home/drew/src/foo" {
		t.Fatalf("cwd %q", grouped.Project.CWD)
	}
	sum := sha256.Sum256([]byte("/home/drew/src/foo"))
	if grouped.Project.CWDHash != hex.EncodeToString(sum[:8]) {
		t.Fatalf("cwd hash %s", grouped.Project.CWDHash)
	}
	if grouped.Lineage.ParentNativeID == nil || *grouped.Lineage.ParentNativeID != "parent-1" {
		t.Fatalf("parent: %+v", grouped.Lineage)
	}
	if string(grouped.Lineage.ForkPoint) != "4" {
		t.Fatalf("fork_point %s", grouped.Lineage.ForkPoint)
	}
}

func TestCWDHashEmpty(t *testing.T) {
	if CWDHash("") != "" {
		t.Fatal("empty cwd should not hash to a bucket")
	}
	if len(CWDHash("/tmp")) != 16 {
		t.Fatal("cwd hash is 8 bytes of hex")
	}
}
