package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetAndRemoveLakeKeepOtherKeys(t *testing.T) {
	dir := t.TempDir()
	env := envOf(map[string]string{"XDG_CONFIG_HOME": dir, "HOME": dir})
	path := filepath.Join(dir, "terva-lampi", "config.json")
	// A fresh machine: no directory, no file.
	lc := LakeConfig{Server: "https://work.example", LakeID: "", KeyID: "", PublicKey: ""}
	if err := SetLake(env, "work", lc); err != nil {
		t.Fatal(err)
	}
	f, err := LoadFile(env)
	if err != nil || f.Lakes["work"].Server != "https://work.example" {
		t.Fatalf("%+v %v", f, err)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	// Keys this release does not know survive a write.
	if err := os.WriteFile(path, []byte(`{"future_key":{"x":1},"projects":{"deny":[{"cwd_prefix":"/s"}]},"lakes":{"work":{"server":"https://work.example"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetLake(env, "home", LakeConfig{Server: "https://home.example"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), `"future_key"`) || !strings.Contains(string(raw), `"/s"`) {
		t.Fatalf("lost keys:\n%s", raw)
	}
	if err := RemoveLake(env, "work"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveLake(env, "work"); err == nil {
		t.Fatal("removed a missing lake")
	}
	if err := RemoveLake(env, "home"); err != nil {
		t.Fatal(err)
	}
	// The last lake gone leaves {}: standalone, not loopback.
	f, _ = LoadFile(env)
	if f.Lakes == nil || len(f.Lakes) != 0 {
		t.Fatalf("lakes %#v", f.Lakes)
	}
	if lakes, _ := ResolveLakes(f, env, LakeFlags{}); len(lakes) != 0 {
		t.Fatalf("empty map resolved to %s", lakeNames(lakes))
	}
	if err := SetLake(env, "Bad", lc); err == nil {
		t.Fatal("bad name written")
	}
	if err := SetLake(env, "x", LakeConfig{Server: "https://x.example", KeyID: "k"}); err == nil {
		t.Fatal("half a pin written")
	}
}

func TestSetLakeOnANullConfigWritesTheLake(t *testing.T) {
	dir := t.TempDir()
	env := envOf(map[string]string{"XDG_CONFIG_HOME": dir, "HOME": dir})
	path := filepath.Join(dir, "terva-lampi", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	// LoadFile reads null as an empty config; a write must too.
	if err := os.WriteFile(path, []byte("null\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetLake(env, "work", LakeConfig{Server: "https://work.example"}); err != nil {
		t.Fatal(err)
	}
	f, err := LoadFile(env)
	if err != nil || f.Lakes["work"].Server != "https://work.example" {
		t.Fatalf("%+v %v", f, err)
	}
}

func TestUpdateLakeEditsTheEntryAsReadAndWritesNothingOnError(t *testing.T) {
	dir := t.TempDir()
	env := envOf(map[string]string{"XDG_CONFIG_HOME": dir, "HOME": dir})
	if err := UpdateLake(env, "work", func(*LakeConfig) error { return nil }); err == nil {
		t.Fatal("updated a missing lake")
	}
	if err := SetLake(env, "work", LakeConfig{Server: "https://work.example", KeyID: "k1", PublicKey: "cDE"}); err != nil {
		t.Fatal(err)
	}
	stop := errors.New("changed")
	err := UpdateLake(env, "work", func(lc *LakeConfig) error {
		if lc.KeyID != "k1" {
			t.Fatalf("edit saw %+v", lc)
		}
		lc.KeyID = "k2"
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatalf("err %v", err)
	}
	if f, _ := LoadFile(env); f.Lakes["work"].KeyID != "k1" {
		t.Fatalf("a refused edit was written: %+v", f.Lakes["work"])
	}
	if err := UpdateLake(env, "work", func(lc *LakeConfig) error { lc.PublicKey = ""; return nil }); err == nil {
		t.Fatal("half a pin written")
	}
	if err := UpdateLake(env, "work", func(lc *LakeConfig) error { lc.KeyID, lc.PublicKey = "k2", "cDI"; return nil }); err != nil {
		t.Fatal(err)
	}
	if f, _ := LoadFile(env); f.Lakes["work"].KeyID != "k2" || f.Lakes["work"].Server != "https://work.example" {
		t.Fatalf("%+v", f.Lakes["work"])
	}
}
