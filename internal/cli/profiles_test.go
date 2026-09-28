package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/audit"
)

func profilesLake(t *testing.T) (dir string, run func(stdin string, args ...string) (string, error)) {
	t.Helper()
	dir = t.TempDir()
	lake, err := api.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	lake.Close()
	return dir, func(stdin string, args ...string) (string, error) {
		var out bytes.Buffer
		env := Env{Stdout: &out, Stderr: ioDiscard(), Stdin: strings.NewReader(stdin)}
		err := Run(append([]string{"serve", "profiles"}, append(args, "--data", dir)...), env)
		return out.String(), err
	}
}

func TestServeProfilesImportSetShowDelete(t *testing.T) {
	dir, run := profilesLake(t)
	file := filepath.Join(t.TempDir(), "profiles.json")
	write := func(body string) {
		if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// One bad profile stops the whole import.
	write(`{"profiles":{"default":{"projects":{"allow":[{"cwd_prefix":"/work"}]}},"ci":{"redaction":{"upload_hits":true}}}}`)
	if _, err := run("", "import", file); err == nil || !strings.Contains(err.Error(), "nothing was imported") {
		t.Fatalf("bad import: %v", err)
	}
	if out, _ := run("", "list"); !strings.Contains(out, "no profiles") {
		t.Fatalf("list after a refused import:\n%s", out)
	}

	write(`{"profiles":{"default":{"projects":{"allow":[{"cwd_prefix":"/work"}]}},"ci":{"agent":{"debounce":"2s"}}}}`)
	out, err := run("", "import", file, "--note", "from the old profiles.json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "imported ci revision") || !strings.Contains(out, "imported default revision") {
		t.Fatalf("import:\n%s", out)
	}
	// The same file again changes nothing.
	if out, err := run("", "import", file); err != nil || strings.Count(out, "unchanged") != 2 {
		t.Fatalf("re-import %v:\n%s", err, out)
	}
	if out, _ := run("", "list"); !strings.Contains(out, "ci version=sha256:") || !strings.Contains(out, "by=serve profiles import") {
		t.Fatalf("list:\n%s", out)
	}
	if out, _ := run("", "show", "default"); !strings.Contains(out, `"cwd_prefix": "/work"`) {
		t.Fatalf("show:\n%s", out)
	}

	if out, err := run(`{"agent":{"debounce":"3s"}}`, "set", "ci", "-", "--note", "slower CI box"); err != nil || !strings.Contains(out, "set ci revision") {
		t.Fatalf("set %v:\n%s", err, out)
	}
	if _, err := run(`{"redaction":{"upload_hits":true}}`, "set", "ci", "-"); err == nil {
		t.Fatal("set took a field a profile cannot hold")
	}
	if _, err := run("", "delete", "default"); err == nil {
		t.Fatal("deleted default")
	}
	if out, err := run("", "delete", "ci", "--note", "box retired"); err != nil || !strings.Contains(out, "deleted ci") {
		t.Fatalf("delete %v:\n%s", err, out)
	}
	out, err = run("", "show", "ci", "--revisions")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "revision=") != 3 || !strings.Contains(out, `note="slower CI box"`) || !strings.Contains(out, "deleted") {
		t.Fatalf("revisions:\n%s", out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, audit.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), string(audit.ProfilePut)); n != 3 {
		t.Fatalf("%d profile.put audit lines, want 3:\n%s", n, raw)
	}
}

func TestServeWarnsThatAProfilesFileIsNotInForce(t *testing.T) {
	data := t.TempDir()
	var out bytes.Buffer
	env := Env{Stdout: ioDiscard(), Stderr: &out}
	warnProfilesFile(env, data, "")
	if out.Len() != 0 {
		t.Fatalf("warned with no file:\n%s", out.String())
	}
	if err := os.WriteFile(filepath.Join(data, "profiles.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	warnProfilesFile(env, data, "/etc/lampi/profiles.json")
	got := out.String()
	for _, want := range []string{
		"/etc/lampi/profiles.json is NOT in force",
		filepath.Join(data, "profiles.json") + " is NOT in force",
		"serve profiles import",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}
