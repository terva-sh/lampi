package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"terva.sh/lampi/internal/protocol"
)

func TestEnsureMachineStable(t *testing.T) {
	dir := t.TempDir()
	getenv := func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			return dir
		}
		return ""
	}
	now = func() time.Time { return time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { now = time.Now })

	first, err := EnsureMachine(getenv)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.MachineID) != 26 {
		t.Fatalf("machine id %q", first.MachineID)
	}
	st, err := os.Stat(filepath.Join(dir, "terva-lampi", "machine.json"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}
	second, err := EnsureMachine(getenv)
	if err != nil {
		t.Fatal(err)
	}
	if second.MachineID != first.MachineID {
		t.Fatalf("id changed %s -> %s", first.MachineID, second.MachineID)
	}
}

func TestLoadFileMissing(t *testing.T) {
	dir := t.TempDir()
	f, err := LoadFile(func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			return dir
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.Server != "" {
		t.Fatalf("server %q", f.Server)
	}
	if len(f.Harnesses) != 0 {
		t.Fatalf("harnesses %#v", f.Harnesses)
	}
	if !f.Harnesses.Enabled(protocol.HarnessTerva) {
		t.Fatal("missing config should leave harnesses default-on")
	}
	if got := ServerURL(f, ""); got != DefaultServer {
		t.Fatalf("default server %s", got)
	}
	if got := ServerURL(File{Server: "http://lake.example"}, ""); got != "http://lake.example" {
		t.Fatalf("file server %s", got)
	}
	if got := ServerURL(File{Server: "http://lake.example"}, "http://other"); got != "http://other" {
		t.Fatalf("flag server %s", got)
	}
}
