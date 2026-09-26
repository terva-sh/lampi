package config

import (
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
