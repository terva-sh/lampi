//go:build unix

package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
)

// fakeRelease serves the two GitHub endpoints install.sh reads: the
// latest-release API and a download directory per tag. Its archive holds
// a shell script standing in for the binary.
type fakeRelease struct {
	tag      string
	archive  []byte
	sums     string
	requests atomic.Int32
	latest   atomic.Int32
}

func newFakeRelease(t *testing.T, tag string) *fakeRelease {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte("#!/bin/sh\necho 'terva-lampi " + tag + " (0123456789ab)'\n")
	if err := tw.WriteHeader(&tar.Header{Name: "terva-lampi", Mode: 0o755, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return &fakeRelease{
		tag:     tag,
		archive: buf.Bytes(),
		sums:    hex.EncodeToString(sum[:]) + "  " + installAsset(tag) + "\n",
	}
}

func installAsset(tag string) string {
	arch := runtime.GOARCH
	return fmt.Sprintf("terva-lampi_%s_%s_%s.tar.gz", strings.TrimPrefix(tag, "v"), runtime.GOOS, arch)
}

func (f *fakeRelease) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.requests.Add(1)
	switch r.URL.Path {
	case "/api/repos/terva-sh/lampi/releases/latest":
		f.latest.Add(1)
		fmt.Fprintf(w, `{"id": 1, "tag_name": %q, "name": %q}`, f.tag, f.tag)
	case "/download/" + f.tag + "/" + installAsset(f.tag):
		w.Write(f.archive)
	case "/download/" + f.tag + "/checksums.txt":
		fmt.Fprint(w, f.sums)
	default:
		http.NotFound(w, r)
	}
}

// runInstaller runs install.sh with no controlling terminal, as it
// would run under `curl | sh` in CI or over ssh without -t.
func runInstaller(t *testing.T, srv *httptest.Server, home string, args ...string) (string, error) {
	t.Helper()
	switch runtime.GOARCH {
	case "amd64", "arm64":
	default:
		t.Skipf("install.sh has no release for %s", runtime.GOARCH)
	}
	script := filepath.Join(repoRoot(t), "install.sh")
	cmd := exec.Command("sh", append([]string{script}, args...)...)
	cmd.Env = []string{
		"HOME=" + home,
		"PATH=" + os.Getenv("PATH"),
		"TMPDIR=" + t.TempDir(),
		"TERVA_LAMPI_INSTALL_API=" + srv.URL + "/api",
		"TERVA_LAMPI_INSTALL_DOWNLOAD=" + srv.URL + "/download",
	}
	cmd.Stdin = strings.NewReader("")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestInstallScriptInstallsTheLatestRelease(t *testing.T) {
	rel := newFakeRelease(t, "v0.1.0")
	srv := httptest.NewServer(rel)
	defer srv.Close()
	home := t.TempDir()

	out, err := runInstaller(t, srv, home)
	if err != nil {
		t.Fatalf("install.sh: %v\n%s", err, out)
	}
	bin := filepath.Join(home, ".local", "bin", "terva-lampi")
	got, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("installed binary: %v", err)
	}
	if !strings.Contains(string(got), "v0.1.0") {
		t.Fatalf("installed binary says %q", got)
	}
	for _, want := range []string{"sha256 verified", "installed " + bin, "terva-lampi register --install-service"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(bin))
	if len(entries) != 1 {
		t.Errorf("the prefix holds %d entries, want only the binary", len(entries))
	}
}

func TestInstallScriptPinsAVersionAndPrefix(t *testing.T) {
	rel := newFakeRelease(t, "v0.2.0")
	srv := httptest.NewServer(rel)
	defer srv.Close()
	prefix := filepath.Join(t.TempDir(), "bin")

	out, err := runInstaller(t, srv, t.TempDir(), "--version", "0.2.0", "--prefix", prefix)
	if err != nil {
		t.Fatalf("install.sh: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(prefix, "terva-lampi")); err != nil {
		t.Fatal(err)
	}
	if n := rel.latest.Load(); n != 0 {
		t.Errorf("a pinned version still asked for the latest release %d times", n)
	}
}

func TestInstallScriptRefusesABadChecksum(t *testing.T) {
	rel := newFakeRelease(t, "v0.1.0")
	rel.sums = strings.Repeat("0", 64) + "  " + installAsset("v0.1.0") + "\n"
	srv := httptest.NewServer(rel)
	defer srv.Close()
	home := t.TempDir()

	out, err := runInstaller(t, srv, home)
	if err == nil {
		t.Fatalf("install.sh accepted a bad checksum:\n%s", out)
	}
	if !strings.Contains(out, "sha256 verification failed") {
		t.Errorf("output does not name the checksum:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "bin", "terva-lampi")); !os.IsNotExist(err) {
		t.Errorf("a binary was installed despite the bad checksum: %v", err)
	}
}

func TestInstallScriptRefusesAnAssetMissingFromChecksums(t *testing.T) {
	rel := newFakeRelease(t, "v0.1.0")
	rel.sums = strings.Repeat("0", 64) + "  terva-lampi_0.1.0_plan9_amd64.tar.gz\n"
	srv := httptest.NewServer(rel)
	defer srv.Close()

	out, err := runInstaller(t, srv, t.TempDir())
	if err == nil || !strings.Contains(out, "sha256 verification failed") {
		t.Fatalf("install.sh did not refuse an unlisted asset: %v\n%s", err, out)
	}
}

// Under `curl | sh` stdin is the script, so --register without a
// terminal must stop before anything is fetched or replaced.
func TestInstallScriptRegisterNeedsATerminal(t *testing.T) {
	rel := newFakeRelease(t, "v0.1.0")
	srv := httptest.NewServer(rel)
	defer srv.Close()
	home := t.TempDir()

	out, err := runInstaller(t, srv, home, "--register")
	if err == nil {
		t.Fatalf("install.sh --register ran without a terminal:\n%s", out)
	}
	if !strings.Contains(out, "--register needs a terminal") {
		t.Errorf("output does not explain the refusal:\n%s", out)
	}
	if n := rel.requests.Load(); n != 0 {
		t.Errorf("the refusal came after %d requests, want none", n)
	}
}

func TestInstallScriptRejectsLakeWithoutRegister(t *testing.T) {
	srv := httptest.NewServer(newFakeRelease(t, "v0.1.0"))
	defer srv.Close()
	out, err := runInstaller(t, srv, t.TempDir(), "--lake", "work")
	if err == nil || !strings.Contains(out, "--lake only applies with --register") {
		t.Fatalf("install.sh accepted --lake alone: %v\n%s", err, out)
	}
}
