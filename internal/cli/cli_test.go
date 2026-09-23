package cli

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/auth"
	"terva.sh/lampi/internal/outbox"
	"terva.sh/lampi/internal/upload"
)

func TestRootHelpListsCommands(t *testing.T) {
	var out bytes.Buffer
	err := Run([]string{"--help"}, Env{Stdout: &out, Stderr: &out})
	if err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, cmd := range []string{"serve", "agent", "sync", "status", "login", "export", "conflicts"} {
		if !strings.Contains(text, "terva-lampi "+cmd) {
			t.Fatalf("help missing %s:\n%s", cmd, text)
		}
	}
	if !strings.Contains(text, "not the\nprimary name") && !strings.Contains(text, "not the primary name") {
		t.Fatalf("help should say lampi is not the primary name:\n%s", text)
	}
	if strings.Contains(text, "primary command is `lampi`") || strings.Contains(text, "primary binary is lampi") {
		t.Fatalf("help treats bare lampi as primary:\n%s", text)
	}
}

func TestAliasWarning(t *testing.T) {
	var errb bytes.Buffer
	if err := Run([]string{"--help"}, Env{Stdout: ioDiscard(), Stderr: &errb, Argv0: "/usr/local/bin/lampi"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errb.String(), "neurobin") {
		t.Fatalf("stderr: %s", errb.String())
	}
	errb.Reset()
	if err := Run([]string{"--help"}, Env{Stdout: ioDiscard(), Stderr: &errb, Argv0: "terva-lampi"}); err != nil {
		t.Fatal(err)
	}
	if errb.Len() != 0 {
		t.Fatalf("unexpected warning: %s", errb.String())
	}
}

func TestServeRefusesNonLoopbackWithoutToken(t *testing.T) {
	data := t.TempDir()
	err := Run([]string{"serve", "--addr", "0.0.0.0:8787", "--data", data}, Env{
		Stdout: ioDiscard(),
		Stderr: ioDiscard(),
	})
	if err == nil || !strings.Contains(err.Error(), "0.0.0.0:8787") || !strings.Contains(err.Error(), "token") {
		t.Fatalf("err %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(data, "catalog.db")); !os.IsNotExist(statErr) {
		t.Fatalf("refused serve created a catalog: %v", statErr)
	}
	if err := refuseExposedWithoutToken("127.0.0.1:8787", nil); err != nil {
		t.Fatal(err)
	}
	devices := &auth.Devices{}
	devices.Allow("device-token")
	if err := refuseExposedWithoutToken("0.0.0.0:8787", devices); err != nil {
		t.Fatal(err)
	}
}

func TestListenLoopback(t *testing.T) {
	cases := []struct {
		addr string
		ok   bool
	}{
		{"127.0.0.1:8787", true},
		{"127.0.0.2:1", true},
		{"[::1]:8787", true},
		{"0.0.0.0:8787", false},
		{"[::]:8787", false},
		{":8787", false},
	}
	for _, tc := range cases {
		got, err := listenLoopback(tc.addr)
		if err != nil {
			t.Fatalf("%s: %v", tc.addr, err)
		}
		if got != tc.ok {
			t.Fatalf("%s: got %v", tc.addr, got)
		}
	}
}

func TestDeviceTokenIsNotAnArgument(t *testing.T) {
	env := Env{Stdout: ioDiscard(), Stderr: ioDiscard(), Getenv: xdg(t.TempDir())}
	for _, args := range [][]string{
		{"sync", "--token", "sekret"},
		{"sync", "--token=sekret"},
		{"status", "--token", "sekret"},
		{"serve", "--token", "sekret"},
		{"agent", "--token", "sekret"},
		{"login", "--token", "sekret"},
		{"login", "-token", "sekret"},
		{"conflicts", "--token", "sekret"},
	} {
		err := Run(args, env)
		if err == nil || !strings.Contains(err.Error(), "--token-file") {
			t.Fatalf("%v: %v", args, err)
		}
		if strings.Contains(err.Error(), "sekret") {
			t.Fatalf("%v echoed the token: %v", args, err)
		}
	}
}

func TestUnknownCommand(t *testing.T) {
	err := Run([]string{"pond"}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()})
	if err == nil || !strings.Contains(err.Error(), "pond") {
		t.Fatalf("err %v", err)
	}
}

func TestLoginDoesNotPrintToken(t *testing.T) {
	cfg := t.TempDir()
	var out bytes.Buffer
	env := Env{
		Stdout: &out,
		Stderr: ioDiscard(),
		Getenv: xdg(cfg),
	}
	if err := Run([]string{"login"}, env); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfg, "terva-lampi", "token")
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}
	tok, err := auth.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), tok) {
		t.Fatal("token was printed")
	}
	if err := Run([]string{"login", "super-secret"}, env); err == nil {
		t.Fatal("positional token should be rejected")
	}
}

func TestAgentDiscover(t *testing.T) {
	home := t.TempDir()
	cfg := t.TempDir()
	state := t.TempDir()
	dir := filepath.Join(home, "sessions", "abcd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "s.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	env := Env{
		Stdout: &out,
		Stderr: ioDiscard(),
		Getenv: func(k string) string {
			switch k {
			case "TERVA_HOME":
				return home
			case "XDG_CONFIG_HOME":
				return cfg
			case "XDG_STATE_HOME":
				return state
			default:
				return ""
			}
		},
	}
	if err := Run([]string{"agent", "discover"}, env); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "sessions/abcd/s.jsonl") {
		t.Fatalf("discover: %s", out.String())
	}
	out.Reset()
	if err := Run([]string{"agent", "machine-id"}, env); err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(out.String())
	if len(id) != 26 {
		t.Fatalf("machine id %q", id)
	}
	out.Reset()
	if err := Run([]string{"agent", "machine-id"}, env); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != id {
		t.Fatal("machine id was not stable")
	}
	out.Reset()
	if err := Run([]string{"agent", "status"}, env); err != nil {
		t.Fatal(err)
	}
	status := out.String()
	if !strings.Contains(status, "watch: fsnotify") && !strings.Contains(status, "watch: poll") {
		t.Fatalf("status: %s", status)
	}
	if !strings.Contains(status, "sessions: 1") {
		t.Fatalf("status: %s", status)
	}
	if !strings.Contains(status, "outbox: 0") || !strings.Contains(status, "last_sync: never") {
		t.Fatalf("status: %s", status)
	}
}

func TestAgentDiscoverSidecars(t *testing.T) {
	home := t.TempDir()
	cfg := t.TempDir()
	state := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(home, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("sessions/abcd/s.jsonl", "{}\n")
	write("raati/raati-4.json", "{}")
	write("tasks/tasks-sess.json", "{}")
	write("tasks/notes.txt", "no")
	var out bytes.Buffer
	env := Env{
		Stdout: &out,
		Stderr: ioDiscard(),
		Getenv: func(k string) string {
			switch k {
			case "TERVA_HOME":
				return home
			case "XDG_CONFIG_HOME":
				return cfg
			case "XDG_STATE_HOME":
				return state
			default:
				return ""
			}
		},
	}
	if err := Run([]string{"agent", "discover"}, env); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, rel := range []string{"sessions/abcd/s.jsonl", "raati/raati-4.json", "tasks/tasks-sess.json"} {
		if !strings.Contains(text, rel) {
			t.Fatalf("discover missing %s: %s", rel, text)
		}
	}
	if strings.Contains(text, "notes.txt") {
		t.Fatalf("discover listed junk: %s", text)
	}
	out.Reset()
	if err := Run([]string{"agent", "status"}, env); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "sessions: 3") {
		t.Fatalf("status: %s", out.String())
	}
}

func TestAgentDiscoverOpenCode(t *testing.T) {
	xdg := t.TempDir()
	data := filepath.Join(xdg, "opencode")
	if err := os.MkdirAll(filepath.Join(data, "export"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("{\"info\":{\"id\":\"ses_1\",\"directory\":\"/work/app\"}}\n")
	if err := os.WriteFile(filepath.Join(data, "export", "ses_1.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "opencode.db"), []byte("db"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "opencode.db-wal"), []byte("wal"), 0o644); err != nil {
		t.Fatal(err)
	}
	tervaHome := t.TempDir()
	cfg := t.TempDir()
	state := t.TempDir()
	var out, errb bytes.Buffer
	env := Env{
		Stdout: &out,
		Stderr: &errb,
		Getenv: func(k string) string {
			switch k {
			case "TERVA_HOME":
				return tervaHome
			case "XDG_DATA_HOME":
				return xdg
			case "XDG_CONFIG_HOME":
				return cfg
			case "XDG_STATE_HOME":
				return state
			default:
				return ""
			}
		},
	}
	if err := Run([]string{"agent", "discover"}, env); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "opencode\texport/ses_1.json") {
		t.Fatalf("discover: %s", text)
	}
	if strings.Contains(text, "opencode.db") {
		t.Fatalf("database or wal was listed while an export exists:\n%s", text)
	}
}

func TestAgentDiscoverCursor(t *testing.T) {
	xdg := t.TempDir()
	root := filepath.Join(xdg, "Cursor")
	global := filepath.Join(root, "User", "globalStorage")
	ws := filepath.Join(root, "User", "workspaceStorage", "ws1")
	for _, dir := range []string{global, ws} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(global, "state.vscdb"), []byte("db"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(global, "state.vscdb-wal"), []byte("wal"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "state.vscdb"), []byte("db"), 0o644); err != nil {
		t.Fatal(err)
	}
	tervaHome := t.TempDir()
	state := t.TempDir()
	var out, errb bytes.Buffer
	env := Env{
		Stdout: &out,
		Stderr: &errb,
		Getenv: func(k string) string {
			switch k {
			case "TERVA_HOME":
				return tervaHome
			case "XDG_CONFIG_HOME":
				return xdg
			case "XDG_STATE_HOME":
				return state
			default:
				return ""
			}
		},
	}
	if err := Run([]string{"agent", "discover"}, env); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, rel := range []string{
		"cursor\tUser/globalStorage/state.vscdb",
		"cursor\tUser/workspaceStorage/ws1/state.vscdb",
	} {
		if !strings.Contains(text, rel) {
			t.Fatalf("discover missing %s: %s", rel, text)
		}
	}
	if strings.Contains(text, "state.vscdb-wal") || strings.Contains(text, "state.vscdb-shm") {
		t.Fatalf("sidecar was listed:\n%s", text)
	}
}

func TestAgentDiscoverCursorCLI(t *testing.T) {
	xdg := t.TempDir()
	session := filepath.Join(xdg, "cursor", "chats", "ab12", "sid-1")
	if err := os.MkdirAll(session, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(session, "store.db"), []byte("db"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(session, "store.db-wal"), []byte("wal"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xdg, "cursor", "auth.json"), []byte(`{"accessToken":"sekret"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	ide := filepath.Join(xdg, "Cursor", "User", "globalStorage")
	if err := os.MkdirAll(ide, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ide, "state.vscdb"), []byte("db"), 0o644); err != nil {
		t.Fatal(err)
	}
	tervaHome := t.TempDir()
	state := t.TempDir()
	var out, errb bytes.Buffer
	env := Env{
		Stdout: &out,
		Stderr: &errb,
		Getenv: func(k string) string {
			switch k {
			case "TERVA_HOME":
				return tervaHome
			case "XDG_CONFIG_HOME":
				return xdg
			case "XDG_STATE_HOME":
				return state
			default:
				return ""
			}
		},
	}
	if err := Run([]string{"agent", "discover"}, env); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "cursor-cli\tchats/ab12/sid-1/store.db") {
		t.Fatalf("discover missing cli store: %s", text)
	}
	if !strings.Contains(text, "cursor\tUser/globalStorage/state.vscdb") {
		t.Fatalf("discover missing ide database: %s", text)
	}
	if strings.Contains(text, "store.db-wal") || strings.Contains(text, "auth.json") {
		t.Fatalf("sidecar or auth file was listed:\n%s", text)
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "cursor\t") && strings.Contains(line, "store.db") {
			t.Fatalf("ide harness listed a cli store: %s", line)
		}
		if strings.HasPrefix(line, "cursor-cli\t") && strings.Contains(line, "state.vscdb") {
			t.Fatalf("cli harness listed an ide database: %s", line)
		}
	}
}

func TestStatusHealth(t *testing.T) {
	lake, err := api.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)

	cfg := t.TempDir()
	home := t.TempDir()
	state := t.TempDir()
	var out bytes.Buffer
	err = Run([]string{"status", "--server", srv.URL}, Env{
		Stdout: &out,
		Stderr: ioDiscard(),
		Getenv: statusEnv(cfg, home, state),
	})
	if err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"health: ok",
		"outbox: 0",
		"watermarks: 0",
		"last_sync: never",
		"catalog_sessions: 0",
		"catalog_artifacts: 0",
		"catalog_machines: 0",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q\n%s", want, text)
		}
	}
}

func TestStatusReportsAgentAndServer(t *testing.T) {
	data := t.TempDir()
	lake, err := api.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	tok := "abc123"
	if err := auth.Write(filepath.Join(data, "token"), tok); err != nil {
		t.Fatal(err)
	}
	lake.Allow(tok)
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)

	home := t.TempDir()
	cfg := t.TempDir()
	state := t.TempDir()
	dir := filepath.Join(home, "sessions", "abcd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"sess-1\",\"cwd\":\"/work/app\"}}\n")
	if err := os.WriteFile(filepath.Join(dir, "sess-1.jsonl"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	tokenCopy := filepath.Join(cfg, "token")
	if err := auth.Write(tokenCopy, tok); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cfg, "terva-lampi"), 0o700); err != nil {
		t.Fatal(err)
	}
	allow := []byte("{\"projects\":{\"allow\":[{\"cwd_prefix\":\"/work/app\"}]}}\n")
	if err := os.WriteFile(filepath.Join(cfg, "terva-lampi", "config.json"), allow, 0o600); err != nil {
		t.Fatal(err)
	}
	env := Env{
		Stdout: ioDiscard(),
		Stderr: ioDiscard(),
		Getenv: statusEnv(cfg, home, state),
	}
	if err := Run([]string{"sync", "--server", srv.URL, "--token-file", tokenCopy}, env); err != nil {
		t.Fatal(err)
	}
	q, err := outbox.Open(outbox.File(filepath.Join(state, "terva-lampi")))
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(context.Background(), outbox.Item{Digest: strings.Repeat("ab", 32)}); err != nil {
		t.Fatal(err)
	}
	q.Close()

	var out bytes.Buffer
	env.Stdout = &out
	if err := Run([]string{"status", "--server", srv.URL, "--token-file", tokenCopy}, env); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"machine_id: ",
		"outbox: 1",
		"watermarks: 1 paths,",
		"newest ",
		"uploaded=1 manifests=1 refused=0 quarantined=0",
		"health: ok",
		"catalog_sessions: 1",
		"catalog_artifacts: 1",
		"catalog_machines: 1",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q\n%s", want, text)
		}
	}
	if strings.Contains(text, "last_sync: never") {
		t.Fatalf("status:\n%s", text)
	}
	if !strings.Contains(text, "last_sync: 20") {
		t.Fatalf("status:\n%s", text)
	}
}

func TestStatusCatalogUnauthorized(t *testing.T) {
	lake, err := api.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	lake.Allow("sekret")
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)

	var out bytes.Buffer
	err = Run([]string{"status", "--server", srv.URL}, Env{
		Stdout: &out,
		Stderr: ioDiscard(),
		Getenv: statusEnv(t.TempDir(), t.TempDir(), t.TempDir()),
	})
	if err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "health: ok") || !strings.Contains(text, "catalog: unauthorized") {
		t.Fatalf("status:\n%s", text)
	}
	if strings.Contains(text, "catalog_sessions:") {
		t.Fatalf("unauthorized status printed counts:\n%s", text)
	}
}

func TestStatusLakeDownStillPrintsLocal(t *testing.T) {
	var out bytes.Buffer
	err := Run([]string{"status", "--server", "http://127.0.0.1:1"}, Env{
		Stdout: &out,
		Stderr: ioDiscard(),
		Getenv: statusEnv(t.TempDir(), t.TempDir(), t.TempDir()),
	})
	if err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"outbox: 0",
		"watermarks: 0",
		"last_sync: never",
		"health: unreachable",
		"catalog: unreachable",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q\n%s", want, text)
		}
	}
}

func TestStatusPrintsRefusalOnLastSync(t *testing.T) {
	home := t.TempDir()
	cfg := t.TempDir()
	state := t.TempDir()
	dir := filepath.Join(home, "sessions", "abcd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"sess-1\",\"cwd\":\"/work/app\"}}\n")
	if err := os.WriteFile(filepath.Join(dir, "sess-1.jsonl"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	env := Env{
		Stdout: ioDiscard(),
		Stderr: ioDiscard(),
		Getenv: statusEnv(cfg, home, state),
	}
	err := Run([]string{"sync", "--server", "http://127.0.0.1:1"}, env)
	if err == nil || !strings.Contains(err.Error(), "not allowlisted") {
		t.Fatalf("err %v", err)
	}
	var out bytes.Buffer
	env.Stdout = &out
	if err := Run([]string{"status", "--server", "http://127.0.0.1:1"}, env); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "uploaded=0 manifests=0 refused=1 quarantined=0") {
		t.Fatalf("status:\n%s", out.String())
	}
}

func TestStatusUnreadableLastSync(t *testing.T) {
	cfg := t.TempDir()
	home := t.TempDir()
	state := t.TempDir()
	dir := filepath.Join(state, "terva-lampi")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "last_sync.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := Run([]string{"status", "--server", "http://127.0.0.1:1"}, Env{
		Stdout: &out,
		Stderr: ioDiscard(),
		Getenv: statusEnv(cfg, home, state),
	})
	if err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"last_sync: unreadable", "outbox: 0", "health: unreachable"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q\n%s", want, text)
		}
	}
}

func statusEnv(cfg, home, state string) func(string) string {
	return func(k string) string {
		switch k {
		case "XDG_CONFIG_HOME":
			return cfg
		case "TERVA_HOME":
			return home
		case "XDG_STATE_HOME":
			return state
		default:
			return ""
		}
	}
}

func TestPrintSyncWarnsOnClockSkew(t *testing.T) {
	var out, errb bytes.Buffer
	printSync(&out, &errb, "", upload.Result{
		Warning: "upload: clock skew 10m0s from server_time 2026-09-22T16:00:00Z",
		Checked: 1,
	})
	if !strings.Contains(errb.String(), "clock skew") || !strings.Contains(errb.String(), "server_time") {
		t.Fatalf("stderr: %s", errb.String())
	}
	if !strings.Contains(out.String(), "checked 1") {
		t.Fatalf("stdout: %s", out.String())
	}
}

func TestSyncAgainstServe(t *testing.T) {
	data := t.TempDir()
	lake, err := api.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	tok := "abc123"
	if err := auth.Write(filepath.Join(data, "token"), tok); err != nil {
		t.Fatal(err)
	}
	lake.Allow(tok)
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)

	home := t.TempDir()
	cfg := t.TempDir()
	state := t.TempDir()
	dir := filepath.Join(home, "sessions", "abcd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "{\"type\":\"meta\",\"meta\":{\"id\":\"sess-1\",\"cwd\":\"/work/app\"}}\n"
	if err := os.WriteFile(filepath.Join(dir, "sess-1.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tokenCopy := filepath.Join(cfg, "token")
	if err := auth.Write(tokenCopy, tok); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cfg, "terva-lampi"), 0o700); err != nil {
		t.Fatal(err)
	}
	allow := []byte("{\"projects\":{\"allow\":[{\"cwd_prefix\":\"/work/app\"}]}}\n")
	if err := os.WriteFile(filepath.Join(cfg, "terva-lampi", "config.json"), allow, 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	env := Env{
		Stdout: &out,
		Stderr: ioDiscard(),
		Getenv: func(k string) string {
			switch k {
			case "TERVA_HOME":
				return home
			case "XDG_CONFIG_HOME":
				return cfg
			case "XDG_STATE_HOME":
				return state
			default:
				return ""
			}
		},
	}
	if err := Run([]string{"sync", "--server", srv.URL, "--token-file", tokenCopy}, env); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "uploaded 1") {
		t.Fatalf("sync: %s", out.String())
	}
	out.Reset()
	if err := Run([]string{"sync", "--server", srv.URL, "--token-file", tokenCopy}, env); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "uploaded 0") {
		t.Fatalf("resync: %s", out.String())
	}
}

func TestSyncRefusesWithoutAllowlist(t *testing.T) {
	data := t.TempDir()
	lake, err := api.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)

	home := t.TempDir()
	cfg := t.TempDir()
	state := t.TempDir()
	dir := filepath.Join(home, "sessions", "abcd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "{\"type\":\"meta\",\"meta\":{\"id\":\"sess-1\",\"cwd\":\"/work/app\"}}\n"
	if err := os.WriteFile(filepath.Join(dir, "sess-1.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = Run([]string{"sync", "--server", srv.URL}, Env{
		Stdout: &out,
		Stderr: ioDiscard(),
		Getenv: func(k string) string {
			switch k {
			case "TERVA_HOME":
				return home
			case "XDG_CONFIG_HOME":
				return cfg
			case "XDG_STATE_HOME":
				return state
			default:
				return ""
			}
		},
	})
	if err == nil || !strings.Contains(err.Error(), "not allowlisted") {
		t.Fatalf("err %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "refused 1") {
		t.Fatalf("summary: %s", out.String())
	}
}

func xdg(cfg string) func(string) string {
	return func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			return cfg
		}
		return ""
	}
}

func ioDiscard() *bytes.Buffer {
	return &bytes.Buffer{}
}
