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
	none := func(string) string { return "" }
	if got := ResolveServer(f, none, ""); got != (Setting{DefaultServer, SourceDefault}) {
		t.Fatalf("default server %+v", got)
	}
	if got := ResolveServer(File{Server: "http://lake.example"}, none, ""); got != (Setting{"http://lake.example", SourceConfig}) {
		t.Fatalf("file server %+v", got)
	}
	if got := ResolveServer(File{Server: "http://lake.example"}, none, "http://other"); got != (Setting{"http://other", SourceFlag}) {
		t.Fatalf("flag server %+v", got)
	}
}

// Flag, then env, then config.json, then the default, for both the
// server and the token file. The env layer was once only the agent's.
func TestResolveOrder(t *testing.T) {
	cfg := t.TempDir()
	env := map[string]string{"XDG_CONFIG_HOME": cfg}
	getenv := func(k string) string { return env[k] }
	file := File{Server: "https://config.example", TokenFile: "/config/token"}

	tok, err := ResolveTokenFile(File{}, getenv, "")
	if err != nil || tok != (Setting{filepath.Join(cfg, "terva-lampi", "token"), SourceDefault}) {
		t.Fatalf("default token %+v %v", tok, err)
	}
	if got := ResolveServer(file, getenv, ""); got.Source != SourceConfig {
		t.Fatalf("config server %+v", got)
	}
	if tok, _ := ResolveTokenFile(file, getenv, ""); tok != (Setting{"/config/token", SourceConfig}) {
		t.Fatalf("config token %+v", tok)
	}
	env["LAMPI_SERVER"] = "https://env.example"
	env["LAMPI_TOKEN_FILE"] = "/env/token"
	if got := ResolveServer(file, getenv, ""); got != (Setting{"https://env.example", SourceEnv}) {
		t.Fatalf("env server %+v", got)
	}
	if tok, _ := ResolveTokenFile(file, getenv, ""); tok != (Setting{"/env/token", SourceEnv}) {
		t.Fatalf("env token %+v", tok)
	}
	if got := ResolveServer(file, getenv, "https://flag.example"); got != (Setting{"https://flag.example", SourceFlag}) {
		t.Fatalf("flag server %+v", got)
	}
	if tok, _ := ResolveTokenFile(file, getenv, "/flag/token"); tok != (Setting{"/flag/token", SourceFlag}) {
		t.Fatalf("flag token %+v", tok)
	}
}
