package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/release"
)

// fakeReleases serves a releases API whose latest is latest, and one
// archive per version in reports, holding a script that says it is
// that version. corrupt names an asset whose checksum line is wrong.
func fakeReleases(t *testing.T, latest string, reports map[string]string, corrupt string) *httptest.Server {
	t.Helper()
	files := map[string][]byte{}
	for tag, says := range reports {
		v, _ := release.Parse(tag)
		asset := releaseAsset(v, runtime.GOOS, runtime.GOARCH)
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		script := []byte("#!/bin/sh\necho 'terva-lampi " + says + " (abc)'\n")
		tw.WriteHeader(&tar.Header{Name: "terva-lampi", Mode: 0o755, Size: int64(len(script)), Typeflag: tar.TypeReg})
		tw.Write(script)
		tw.Close()
		gz.Close()
		sum := sha256.Sum256(buf.Bytes())
		line := hex.EncodeToString(sum[:])
		if asset == corrupt {
			line = strings.Repeat("0", 64)
		}
		files["/dl/"+tag+"/"+asset] = buf.Bytes()
		files["/dl/"+tag+"/checksums.txt"] = []byte(line + "  " + asset + "\n")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/terva-sh/lampi/releases/latest" {
			fmt.Fprintf(w, `{"tag_name":%q}`, latest)
			return
		}
		if b, ok := files[r.URL.Path]; ok {
			w.Write(b)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

type selfUpdateRig struct {
	env      Env
	out      *bytes.Buffer
	exe      string
	commands []string
}

// newSelfUpdateRig runs this binary as release running, installed at a
// temp path, with the releases at rel and lake url as its lake (none
// when empty).
func newSelfUpdateRig(t *testing.T, running string, rel *httptest.Server, lakeURL string, serviceUp bool) *selfUpdateRig {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake release binary is a shell script")
	}
	rig := &selfUpdateRig{out: &bytes.Buffer{}}
	rig.exe = filepath.Join(t.TempDir(), "terva-lampi")
	if err := os.WriteFile(rig.exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	home, cfg, state := t.TempDir(), t.TempDir(), t.TempDir()
	if lakeURL != "" {
		home, cfg, state, _ = agentFixture(t, lakeURL)
	}
	base := agentGetenv(home, cfg, state)
	rig.env = Env{Stdout: rig.out, Stderr: rig.out, Getenv: func(k string) string {
		switch k {
		case "TERVA_LAMPI_INSTALL_API":
			return rel.URL
		case "TERVA_LAMPI_INSTALL_DOWNLOAD":
			return rel.URL + "/dl"
		}
		return base(k)
	}}
	oldRel, oldExe, oldRun, oldGOOS := runningRelease, selfExecutable, runCommand, serviceGOOS
	t.Cleanup(func() { runningRelease, selfExecutable, runCommand, serviceGOOS = oldRel, oldExe, oldRun, oldGOOS })
	runningRelease = func() string { return running }
	selfExecutable = func() (string, error) { return rig.exe, nil }
	serviceGOOS = "linux"
	runCommand = func(name string, args ...string) ([]byte, error) {
		cmd := name + " " + strings.Join(args, " ")
		rig.commands = append(rig.commands, cmd)
		if strings.Contains(cmd, "is-active") && !serviceUp {
			return nil, errors.New("inactive")
		}
		return nil, nil
	}
	return rig
}

func (r *selfUpdateRig) run(args ...string) error {
	return Run(append([]string{"self-update"}, args...), r.env)
}

func lakeWithRelease(t *testing.T, rel string) string {
	t.Helper()
	lake, err := api.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	lake.Release = rel
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestSelfUpdateInstallsTheLakesReleaseAndRestartsTheService(t *testing.T) {
	rel := fakeReleases(t, "v0.3.0", map[string]string{"v0.2.0": "v0.2.0", "v0.3.0": "v0.3.0"}, "")
	rig := newSelfUpdateRig(t, "v0.1.3", rel, lakeWithRelease(t, "v0.2.0"), true)
	if err := rig.run(); err != nil {
		t.Fatalf("%v\n%s", err, rig.out)
	}
	got, _ := os.ReadFile(rig.exe)
	if !strings.Contains(string(got), "terva-lampi v0.2.0") {
		t.Fatalf("installed %q, want the lake's v0.2.0\n%s", got, rig.out)
	}
	if prev, _ := os.ReadFile(rig.exe + ".prev"); string(prev) != "old binary" {
		t.Fatalf(".prev holds %q", prev)
	}
	if !strings.Contains(strings.Join(rig.commands, "\n"), "systemctl --user restart terva-lampi-agent.service") {
		t.Fatalf("no restart: %v\n%s", rig.commands, rig.out)
	}
}

func TestSelfUpdateNoRestartAndStoppedServiceLeaveTheServiceAlone(t *testing.T) {
	rel := fakeReleases(t, "v0.3.0", map[string]string{"v0.3.0": "v0.3.0"}, "")
	rig := newSelfUpdateRig(t, "v0.1.3", rel, "", true)
	if err := rig.run("--no-restart"); err != nil {
		t.Fatalf("%v\n%s", err, rig.out)
	}
	if len(rig.commands) != 0 {
		t.Fatalf("ran %v with --no-restart", rig.commands)
	}
	rig = newSelfUpdateRig(t, "v0.1.3", rel, "", false)
	if err := rig.run(); err != nil {
		t.Fatalf("%v\n%s", err, rig.out)
	}
	for _, c := range rig.commands {
		if strings.Contains(c, "restart") {
			t.Fatalf("restarted a stopped service: %v", rig.commands)
		}
	}
}

func TestSelfUpdateCheckExitsByGap(t *testing.T) {
	rel := fakeReleases(t, "v0.3.0", nil, "")
	for running, want := range map[string]int{"v0.2.9": 11, "v0.3.0": 0, "v0.2.0": 11} {
		rig := newSelfUpdateRig(t, running, rel, "", true)
		err := rig.run("--check")
		var st *ExitStatus
		switch {
		case want == 0 && err != nil:
			t.Fatalf("%s: %v", running, err)
		case want != 0 && (!errors.As(err, &st) || st.Code != want):
			t.Fatalf("%s: %v, want exit %d", running, err, want)
		}
	}
	rig := newSelfUpdateRig(t, "v0.3.0", rel, "", true)
	var st *ExitStatus
	if err := rig.run("--check", "--version", "v1.0.0"); !errors.As(err, &st) || st.Code != 12 {
		t.Fatalf("major: %v", err)
	}
	if got, _ := os.ReadFile(rig.exe); string(got) != "old binary" {
		t.Fatal("--check replaced the binary")
	}
}

func TestSelfUpdateRefusals(t *testing.T) {
	rel := fakeReleases(t, "v0.3.0", map[string]string{"v0.3.0": "v0.3.0", "v0.2.0": "v0.9.9"}, releaseAsset(release.Version{Minor: 3}, runtime.GOOS, runtime.GOARCH))
	untouched := func(rig *selfUpdateRig) {
		t.Helper()
		if got, _ := os.ReadFile(rig.exe); string(got) != "old binary" {
			t.Fatalf("binary replaced:\n%s", rig.out)
		}
		if _, err := os.Stat(rig.exe + ".prev"); err == nil {
			t.Fatal(".prev written for an update that did not happen")
		}
	}
	rig := newSelfUpdateRig(t, "0.0.0", rel, "", true)
	if err := rig.run(); err == nil || !strings.Contains(err.Error(), "not a release") {
		t.Fatalf("dev build: %v", err)
	}
	rig = newSelfUpdateRig(t, "v0.1.3", rel, "", true)
	if err := rig.run(); err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("bad checksum: %v", err)
	}
	untouched(rig)
	rig = newSelfUpdateRig(t, "v0.1.3", rel, "", true)
	if err := rig.run("--version", "v0.2.0"); err == nil || !strings.Contains(err.Error(), "nothing was replaced") {
		t.Fatalf("wrong version inside: %v", err)
	}
	untouched(rig)
	// An agent ahead of its lake is not downgraded unless asked.
	rig = newSelfUpdateRig(t, "v0.3.0", rel, lakeWithRelease(t, "v0.2.0"), true)
	if err := rig.run(); err != nil || !strings.Contains(rig.out.String(), "ahead of") {
		t.Fatalf("ahead: %v\n%s", err, rig.out)
	}
	untouched(rig)
}
