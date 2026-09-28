package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/release"
	"terva.sh/lampi/internal/upload"
)

const selfUpdateUsage = `terva-lampi self-update — upgrade this binary to a verified release

usage:
  terva-lampi self-update [--check | --dry-run] [--version TAG | --latest]
                          [--lake NAME] [--no-restart]

With no flag, self-update installs the release the agent's lake runs, so
the agent is never ahead of its lake. The newest release on GitHub caps
it, and is the target on a machine with no lake configured. A lake that
cannot be reached, or that names no release, stops the update. --lake
picks the lake to ask; the first configured lake is the default. --version TAG
installs that release, older or newer. --latest installs the newest
release whatever the lake runs.

The download is checked against the release's checksums.txt before
anything on disk changes, and the new binary must report the expected
version before it replaces this one. The binary it replaces is kept
beside it as terva-lampi.prev.

After an update, a running terva-lampi-agent service (systemd user unit
or launchd agent) is restarted so it runs the new binary. --no-restart
leaves it running the old one until you restart it.

--check says whether an update is available and changes nothing. It
exits 0 when up to date, and 10, 11 or 12 when a patch, minor or major
update is available. --dry-run also names the download and the file it
would replace.

TERVA_LAMPI_INSTALL_API and TERVA_LAMPI_INSTALL_DOWNLOAD point it at
another releases API and download base, as they do for install.sh.
`

const releaseRepo = "terva-sh/lampi"

// smokeTimeout bounds the staged binary's --version run.
var smokeTimeout = 30 * time.Second

// selfExecutable is this binary's path. Tests replace it.
var selfExecutable = func() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

// ExitStatus is a command's answer as an exit status rather than a
// failure: main exits with Code and prints nothing more.
type ExitStatus struct{ Code int }

func (e *ExitStatus) Error() string { return "exit status " + strconv.Itoa(e.Code) }

func gapExit(gap string) int {
	switch gap {
	case "patch":
		return 10
	case "minor":
		return 11
	case "major":
		return 12
	}
	return 0
}

// releaseAsset is the archive goreleaser publishes for a platform.
func releaseAsset(v release.Version, goos, goarch string) string {
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("terva-lampi_%d.%d.%d_%s_%s.%s", v.Major, v.Minor, v.Patch, goos, goarch, ext)
}

func runSelfUpdate(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), selfUpdateUsage)
		return nil
	}
	var check, dryRun, latest, noRestart bool
	var pin, lakeFlag string
	rest, err := parseFlags(env, args, selfUpdateUsage, func(fs *flag.FlagSet) {
		fs.BoolVar(&check, "check", false, "say whether an update is available, and exit by its size")
		fs.BoolVar(&dryRun, "dry-run", false, "also name the download and the file it would replace")
		fs.StringVar(&pin, "version", "", "install this release")
		fs.BoolVar(&latest, "latest", false, "install the newest release, whatever the lake runs")
		fs.StringVar(&lakeFlag, "lake", "", "the lake whose release to install")
		fs.BoolVar(&noRestart, "no-restart", false, "leave a running agent service on the old binary")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), selfUpdateUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	if check && dryRun {
		return errors.New("--check and --dry-run ask the same question at two levels of detail; pick one")
	}
	if pin != "" && latest {
		return errors.New("--version and --latest name two targets; pick one")
	}
	running := runningRelease()
	cur, ok := release.Parse(running)
	if !ok {
		return fmt.Errorf("this binary reports %s, which is not a release: it was built from a working tree, and replacing it would lose that build. Install a release with install.sh", running)
	}
	api := env.getenv("TERVA_LAMPI_INSTALL_API")
	if api == "" {
		api = "https://api.github.com"
	}
	download := env.getenv("TERVA_LAMPI_INSTALL_DOWNLOAD")
	if download == "" {
		download = "https://github.com/" + releaseRepo + "/releases/download"
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	ctx := context.Background()

	var target release.Version
	var why string
	if pin != "" {
		if target, ok = release.Parse(pin); !ok {
			return fmt.Errorf("--version %q is not a release such as v0.1.3", pin)
		}
		why = "the release you named"
	} else {
		newest, err := latestRelease(ctx, client, api, cur)
		if err != nil {
			return err
		}
		target, why = newest, "the newest release"
		if !latest {
			lakeV, name, err := askLakeRelease(ctx, env, lakeFlag)
			switch {
			case errors.Is(err, errNoLakeRelease):
				fmt.Fprintf(env.stderr(), "terva-lampi: no lake is configured; using %s\n", why)
			case err != nil:
				// Guessing would break the one promise a bare run makes:
				// never ahead of the lake.
				return fmt.Errorf("%w; self-update installs the lake's release, so it stops here. Pass --version TAG or --latest to choose a release yourself", err)
			case lakeV.Compare(newest) <= 0:
				target, why = lakeV, "the release lake "+name+" runs"
			}
		}
	}
	gap := release.Gap(cur, target)
	switch c := target.Compare(cur); {
	case c == 0:
		fmt.Fprintf(env.stdout(), "terva-lampi %s is up to date (%s)\n", cur, why)
		return nil
	case c < 0 && pin == "":
		fmt.Fprintf(env.stdout(), "terva-lampi %s is ahead of %s, %s; nothing to do\n", cur, why, target)
		return nil
	}
	if check {
		fmt.Fprintf(env.stdout(), "terva-lampi %s → %s (%s): run terva-lampi self-update\n", cur, target, why)
		if code := gapExit(gap); code != 0 {
			return &ExitStatus{Code: code}
		}
		return nil
	}
	exe, err := selfExecutable()
	if err != nil {
		return fmt.Errorf("cannot find this binary's own path: %w", err)
	}
	asset := releaseAsset(target, runtime.GOOS, runtime.GOARCH)
	if dryRun {
		fmt.Fprintf(env.stdout(), "terva-lampi %s → %s (%s)\nwould download %s/%s/%s and replace %s\n", cur, target, why, download, target, asset, exe)
		if code := gapExit(gap); code != 0 {
			return &ExitStatus{Code: code}
		}
		return nil
	}
	fmt.Fprintf(env.stdout(), "downloading %s (%s, %s)\n", asset, target, why)
	bin, err := fetchVerified(ctx, client, download+"/"+target.String(), asset, cur)
	if err != nil {
		return err
	}
	if err := replaceBinary(exe, bin, target); err != nil {
		return err
	}
	fmt.Fprintf(env.stdout(), "updated %s from %s to %s; the old binary is %s.prev\n", exe, cur, target, filepath.Base(exe))
	if noRestart {
		fmt.Fprintln(env.stdout(), "a running agent keeps the old binary until it restarts")
		return nil
	}
	return restartAgentService(env)
}

// latestRelease asks the releases API for the newest release. The
// API's latest never names a prerelease.
func latestRelease(ctx context.Context, client *http.Client, api string, cur release.Version) (release.Version, error) {
	body, err := fetchURL(ctx, client, strings.TrimRight(api, "/")+"/repos/"+releaseRepo+"/releases/latest", cur, 1<<20)
	if err != nil {
		return release.Version{}, fmt.Errorf("asking for the newest release: %w", err)
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &rel); err != nil {
		return release.Version{}, fmt.Errorf("reading the newest release: %w", err)
	}
	v, ok := release.Parse(rel.TagName)
	if !ok {
		return release.Version{}, fmt.Errorf("the newest release's tag %q is not a release version", rel.TagName)
	}
	return v, nil
}

// errNoLakeRelease is askLakeRelease on a machine with no lake of its
// own, the one case where a bare self-update falls back to the newest
// release.
var errNoLakeRelease = errors.New("no lake is configured")

// askLakeRelease is the release a configured lake names in its hello
// answer: the lake called name, or the first one. A lake that cannot
// be asked, or that names no release, is an error.
func askLakeRelease(ctx context.Context, env Env, name string) (release.Version, string, error) {
	cc, err := loadClientConfig(env, io.Discard, config.LakeFlags{Lake: name})
	if err != nil {
		return release.Version{}, "", err
	}
	if len(cc.lakes) == 0 || (name == "" && !lakeConfigured(cc)) {
		return release.Version{}, "", errNoLakeRelease
	}
	l := cc.lakes[0]
	token, err := lakeToken(l)
	if err != nil {
		return release.Version{}, l.Name, fmt.Errorf("lake %s: %w", l.Name, err)
	}
	hctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	h, err := upload.Hello(hctx, upload.Options{ServerURL: l.Server.Value, Token: token})
	if err != nil {
		return release.Version{}, l.Name, fmt.Errorf("lake %s did not say which release it runs: %w", l.Name, err)
	}
	v, ok := release.Parse(h.Release)
	if !ok {
		return release.Version{}, l.Name, fmt.Errorf("lake %s names no release: it is older than this agent, or not a release build; upgrade the lake first", l.Name)
	}
	return v, l.Name, nil
}

// lakeConfigured reports whether a lake came from somewhere other than
// the built-in loopback default: a flag, the environment, or a file.
func lakeConfigured(cc clientConfig) bool {
	for _, l := range cc.lakes {
		if l.Server.Source != config.SourceDefault {
			return true
		}
	}
	return false
}

// fetchVerified downloads asset and checksums.txt from base and returns
// the terva-lampi binary inside the asset, once its sha256 matches.
func fetchVerified(ctx context.Context, client *http.Client, base, asset string, cur release.Version) ([]byte, error) {
	archive, err := fetchURL(ctx, client, base+"/"+asset, cur, 256<<20)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", asset, err)
	}
	sums, err := fetchURL(ctx, client, base+"/checksums.txt", cur, 1<<20)
	if err != nil {
		return nil, fmt.Errorf("downloading checksums.txt: %w", err)
	}
	want := ""
	for _, line := range strings.Split(string(sums), "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[1] == asset {
			want = f[0]
			break
		}
	}
	if want == "" {
		return nil, fmt.Errorf("checksums.txt has no line for %s; refusing to install it", asset)
	}
	sum := sha256.Sum256(archive)
	if got := hex.EncodeToString(sum[:]); got != want {
		return nil, fmt.Errorf("sha256 mismatch for %s: got %s, checksums.txt says %s; refusing to install it", asset, got, want)
	}
	return extractBinary(asset, archive)
}

func fetchURL(ctx context.Context, client *http.Client, url string, cur release.Version, max int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// The GitHub API requires a User-Agent.
	req.Header.Set("User-Agent", "terva-lampi/"+strings.TrimPrefix(cur.String(), "v"))
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > max {
		return nil, fmt.Errorf("GET %s: more than %d bytes", url, max)
	}
	return body, nil
}

// extractBinary takes terva-lampi from the archive root, where
// .goreleaser.yaml puts it.
func extractBinary(asset string, archive []byte) ([]byte, error) {
	if strings.HasSuffix(asset, ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, fmt.Errorf("opening %s: %w", asset, err)
		}
		for _, f := range zr.File {
			if f.Name == "terva-lampi.exe" {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
		return nil, fmt.Errorf("%s has no terva-lampi.exe at its root", asset)
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", asset, err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s has no terva-lampi at its root", asset)
		}
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", asset, err)
		}
		if hdr.Typeflag == tar.TypeReg && strings.TrimPrefix(hdr.Name, "./") == "terva-lampi" {
			return io.ReadAll(tr)
		}
	}
}

// replaceBinary stages bin beside target, checks that it runs and
// reports want, keeps target as target.prev, and renames the staged
// copy over target. A binary that does not run here, or reports
// another version, replaces nothing. On Unix the old binary is linked
// to .prev, so target never goes missing. Windows cannot replace a
// running executable, so there the old one is moved to .prev first.
func replaceBinary(target string, bin []byte, want release.Version) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".terva-lampi-new-*")
	if err != nil {
		return fmt.Errorf("cannot write beside %s: %w; self-update does not use sudo, so run it as the user who owns that directory", target, err)
	}
	stage := tmp.Name()
	defer os.Remove(stage)
	_, werr := tmp.Write(bin)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return werr
	}
	mode := os.FileMode(0o755)
	if fi, err := os.Stat(target); err == nil {
		mode = fi.Mode().Perm() | 0o100
	}
	if err := os.Chmod(stage, mode); err != nil {
		return err
	}
	sctx, cancel := context.WithTimeout(context.Background(), smokeTimeout)
	defer cancel()
	smoke := exec.CommandContext(sctx, stage, "--version")
	// A child the binary started can hold its output open past the
	// kill; stop waiting for it shortly after.
	smoke.WaitDelay = time.Second
	out, err := smoke.Output()
	if err != nil {
		return fmt.Errorf("the downloaded binary does not run here (%v); nothing was replaced", err)
	}
	var got release.Version
	if f := strings.Fields(string(out)); len(f) >= 2 && f[0] == "terva-lampi" {
		got, _ = release.Parse(f[1])
	}
	if got != want {
		return fmt.Errorf("the downloaded binary reports %q, not %s; nothing was replaced", strings.TrimSpace(string(out)), want)
	}
	// The old binary goes to a side name first. An existing .prev is
	// replaced only once the new binary is in place, so a failed swap
	// leaves both the binary and the last rollback copy as they were.
	prev := target + ".prev"
	side := stage + ".prev"
	defer os.Remove(side)
	if runtime.GOOS == "windows" {
		if err := os.Rename(target, side); err != nil {
			return fmt.Errorf("cannot move the running binary aside: %w", err)
		}
	} else if err := os.Link(target, side); err != nil {
		return fmt.Errorf("cannot keep the old binary as %s: %w; nothing was replaced", prev, err)
	}
	if err := os.Rename(stage, target); err != nil {
		if runtime.GOOS == "windows" {
			os.Rename(side, target)
		}
		return fmt.Errorf("cannot replace %s: %w; self-update does not use sudo, so run it as the user who owns it", target, err)
	}
	if err := os.Rename(side, prev); err != nil {
		return fmt.Errorf("updated %s, but could not keep the old binary as %s: %w", target, prev, err)
	}
	return nil
}

// restartAgentService restarts the agent's user service if one is
// running, so it runs the new binary. HUP is not enough: it reloads
// the lakes in the old process. A stopped service stays stopped. A
// restart that fails is an error: the binary is new, but the service
// still runs the old one.
func restartAgentService(env Env) error {
	var name string
	var args []string
	switch serviceGOOS {
	case "linux":
		if _, err := runCommand("systemctl", "--user", "is-active", "--quiet", systemdUnitName); err != nil {
			fmt.Fprintln(env.stdout(), "no running terva-lampi-agent service; restart any agent you run yourself")
			return nil
		}
		name, args = "systemctl", []string{"--user", "restart", systemdUnitName}
	case "darwin":
		svc := "gui/" + strconv.Itoa(os.Getuid()) + "/" + launchdLabel
		if _, err := runCommand("launchctl", "print", svc); err != nil {
			fmt.Fprintln(env.stdout(), "no loaded terva-lampi agent service; restart any agent you run yourself")
			return nil
		}
		name, args = "launchctl", []string{"kickstart", "-k", svc}
	default:
		fmt.Fprintln(env.stdout(), "restart any running terva-lampi agent so it runs the new binary")
		return nil
	}
	cmd := name + " " + strings.Join(args, " ")
	if out, err := runCommand(name, args...); err != nil {
		return fmt.Errorf("the binary is updated, but %s failed: %v: %s; the service still runs the old binary until you restart it", cmd, err, strings.TrimSpace(string(out)))
	}
	fmt.Fprintf(env.stdout(), "restarted the agent service (%s)\n", cmd)
	return nil
}
