package cursorcli

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/adapter/cursor"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/watch"

	_ "modernc.org/sqlite"
)

func TestHome(t *testing.T) {
	got, err := configDir("linux", func(k string) string {
		switch k {
		case "CURSOR_CONFIG_DIR":
			return "/opt/cursor-cli"
		case "XDG_CONFIG_HOME":
			return "/tmp/xdg"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "/opt/cursor-cli" {
		t.Fatalf("override %s", got)
	}

	got, err = configDir("linux", func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			return "/tmp/xdg"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join("/tmp/xdg", "cursor") {
		t.Fatalf("xdg %s", got)
	}

	got, err = configDir("linux", func(k string) string {
		if k == "HOME" {
			return "/home/drew"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join("/home/drew", ".cursor") {
		t.Fatalf("linux default %s", got)
	}

	got, err = configDir("darwin", func(k string) string {
		switch k {
		case "HOME":
			return "/Users/drew"
		case "XDG_CONFIG_HOME":
			return "/xdg"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join("/Users/drew", ".cursor") {
		t.Fatalf("darwin %s", got)
	}

	got, err = configDir("windows", func(k string) string {
		switch k {
		case "USERPROFILE":
			return `C:\Users\drew`
		case "HOME":
			return `D:\other`
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(`C:\Users\drew`, ".cursor") {
		t.Fatalf("userprofile %s", got)
	}

	got, err = configDir("windows", func(k string) string {
		if k == "HOME" {
			return `C:\Users\drew`
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(`C:\Users\drew`, ".cursor") {
		t.Fatalf("windows home %s", got)
	}

	if _, err := configDir("linux", func(string) string { return "" }); err == nil {
		t.Fatal("missing home should fail")
	}
}

func TestDiscoverListsOnlyChatStores(t *testing.T) {
	root := t.TempDir()
	session := filepath.Join(root, "chats", "ab12", "sid-1")
	mustWrite(t, filepath.Join(session, "store.db"), "db")
	mustWrite(t, filepath.Join(session, "store.db-wal"), "wal")
	mustWrite(t, filepath.Join(session, "store.db-shm"), "shm")
	mustWrite(t, filepath.Join(session, "meta.json"), `{"cwd":"/work/app"}`)
	mustWrite(t, filepath.Join(session, "prompt_history.json"), `[]`)
	mustWrite(t, filepath.Join(root, "auth.json"), `{"accessToken":"sekret"}`)
	mustWrite(t, filepath.Join(root, "cli-config.json"), `{}`)
	mustWrite(t, filepath.Join(root, "chats", "store.db"), "shallow")
	mustWrite(t, filepath.Join(root, "chats", "ab12", "sid-1", "nested", "store.db"), "deep")
	mustWrite(t, filepath.Join(root, "acp-sessions", "sid-1", "store.db"), "acp")
	mustWrite(t, filepath.Join(root, "projects", "work-app", "agent-transcripts", "sid-1.jsonl"), "{}\n")
	mustWrite(t, filepath.Join(root, "User", "globalStorage", "state.vscdb"), "ide")

	refs, err := Adapter{}.Discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].RelPath != "chats/ab12/sid-1/store.db" {
		t.Fatalf("refs %+v", refs)
	}
	if refs[0].Kind != protocol.KindCursorCLIStoreJSON {
		t.Fatalf("kind %s", refs[0].Kind)
	}
	if _, err := (Adapter{}).Discover(context.Background(), filepath.Join(root, "missing")); err != nil {
		t.Fatal(err)
	}

	ide, err := cursor.Adapter{}.Discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(ide) != 1 || ide[0].RelPath != "User/globalStorage/state.vscdb" {
		t.Fatalf("ide refs %+v", ide)
	}
	cliOnIDEPath, err := Adapter{}.Discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range cliOnIDEPath {
		if strings.Contains(r.RelPath, "state.vscdb") {
			t.Fatalf("cli listed an ide database: %s", r.RelPath)
		}
	}
}

func TestSnapshotFiltersAuthAndReadsWAL(t *testing.T) {
	root := t.TempDir()
	session := filepath.Join(root, "chats", "ab12", "sid-1")
	dbPath := filepath.Join(session, "store.db")
	keep, err := seedLive(t, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(session, "meta.json"), []byte(`{"schemaVersion":1,"cwd":"/work/my app","title":"kept"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(root, "chats", "ab12", "no-cwd", "store.db")
	if err := seedClosed(plain); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(plain), "meta.json"), []byte(`{"cwd":"work/app"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	fileURI := filepath.Join(root, "chats", "ab12", "remote", "store.db")
	if err := seedClosed(fileURI); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(fileURI), "meta.json"), []byte(`{"cwd":"file:///work/app"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	before := hashes(t, dbPath)
	rel := "chats/ab12/sid-1/store.db"
	body, err := exportDatabase(context.Background(), dbPath, rel)
	if err != nil {
		t.Fatal(err)
	}
	after := hashes(t, dbPath)
	for name, sum := range before {
		if after[name] != sum {
			t.Fatalf("live %s changed", name)
		}
	}
	if _, err := os.Stat(dbPath + "-journal"); err == nil {
		t.Fatal("export created a live journal")
	}
	text := string(body)
	for _, secret := range []string{"sekret-token", "sekret-refresh", "sekret-blob", "sekret-plain", "cursorAuth"} {
		if strings.Contains(text, secret) {
			t.Fatalf("export contains %s: %s", secret, text)
		}
	}
	hexSecret := hex.EncodeToString([]byte("sekret-token"))
	if strings.Contains(text, hexSecret) {
		t.Fatalf("export kept the hex-encoded secret: %s", text)
	}
	for _, want := range []string{`"harness_version":"1"`, `"confidence":"low"`, `"scope":"session"`, "kept-title", "hello from cli", keep, "1767396459642"} {
		if !strings.Contains(text, want) {
			t.Fatalf("export missing %s: %s", want, text)
		}
	}
	if strings.Contains(text, "the field accessToken is a name") && strings.Contains(text, "sekret") {
		t.Fatal("scrub reached inside a string")
	}
	if !strings.Contains(text, "the field accessToken is a name") {
		t.Fatalf("conversation text was dropped: %s", text)
	}

	alone := filepath.Join(t.TempDir(), "store.db")
	if err := copyFile(dbPath, alone); err != nil {
		t.Fatal(err)
	}
	aloneBody, err := exportDatabase(context.Background(), alone, rel)
	if err == nil && strings.Contains(string(aloneBody), keep) {
		t.Fatal("db copied without its wal still contained the wal-only row")
	}

	b, err := Manifests(root, "machine-1")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Cleanup()
	if len(b.Manifests) != 3 {
		t.Fatalf("manifests %d", len(b.Manifests))
	}
	byID := map[string]protocol.Manifest{}
	for _, m := range b.Manifests {
		byID[m.NativeSessionID] = m
		if m.Harness != protocol.HarnessCursorCLI || m.HarnessVersion != Version || m.CaptureProtocol != 1 {
			t.Fatalf("header %+v", m)
		}
		if m.Harness == protocol.HarnessCursor {
			t.Fatal("cli manifest used the ide harness")
		}
		if len(m.Artifacts) != 1 || m.Artifacts[0].Kind != protocol.KindCursorCLIStoreJSON {
			t.Fatalf("artifact %+v", m.Artifacts)
		}
		raw, err := os.ReadFile(b.Paths[m.Artifacts[0].RelPath])
		if err != nil {
			t.Fatal(err)
		}
		if bytesContainAuth(raw) {
			t.Fatalf("put path contains auth: %s", raw)
		}
		if strings.HasPrefix(string(raw), "SQLite format 3") {
			t.Fatal("put path is the raw database")
		}
	}
	withCWD := byID["chats/ab12/sid-1"]
	if withCWD.Project.CWD != "/work/my app" {
		t.Fatalf("cwd %q", withCWD.Project.CWD)
	}
	if withCWD.Artifacts[0].RelPath != "chats/ab12/sid-1/store.json" {
		t.Fatalf("rel %s", withCWD.Artifacts[0].RelPath)
	}
	if byID["chats/ab12/no-cwd"].Project.CWD != "" {
		t.Fatalf("relative cwd was accepted: %q", byID["chats/ab12/no-cwd"].Project.CWD)
	}
	if byID["chats/ab12/remote"].Project.CWD != "" {
		t.Fatalf("file uri was accepted: %q", byID["chats/ab12/remote"].Project.CWD)
	}

	rc, err := Adapter{}.ReadSlice(context.Background(), dbPath, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	slice, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if bytesContainAuth(slice) || strings.HasPrefix(string(slice), "SQLite format 3") {
		t.Fatal("ReadSlice returned raw database bytes")
	}
	if _, err := (Adapter{}).ReadSlice(context.Background(), dbPath+"-wal", 0); err == nil {
		t.Fatal("wal sidecar was readable")
	}
}

func TestMissingBlobsTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "store.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := exportDatabase(context.Background(), path, "chats/ab/sid/store.db"); err == nil {
		t.Fatal("database without blobs was accepted")
	}
}

func TestClosedDBDoesNotGrowSidecars(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "store.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE blobs (id TEXT PRIMARY KEY, data BLOB)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO meta (key, value) VALUES ('0', 'hello')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := exportDatabase(context.Background(), path, "chats/a/b/store.db"); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Stat(path + suffix); !os.IsNotExist(err) {
			t.Fatalf("live sidecar %s appeared: %v", suffix, err)
		}
	}
}

func TestCopyTrio(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "store.db")
	mustWrite(t, src, "db-bytes")
	mustWrite(t, src+"-wal", "wal-bytes")
	mustWrite(t, src+"-shm", "shm-bytes")
	before := hashes(t, src)
	snap, err := copyTrio(src)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(snap)
	for _, name := range []string{"store.db", "store.db-wal", "store.db-shm"} {
		got, err := os.ReadFile(filepath.Join(snap, name))
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("%s snapshot mismatch", name)
		}
	}
	after := hashes(t, src)
	for name, sum := range before {
		if after[name] != sum {
			t.Fatalf("source %s changed", name)
		}
	}

	only := filepath.Join(t.TempDir(), "store.db")
	mustWrite(t, only, "solo")
	snap, err = copyTrio(only)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(snap)
	if _, err := os.Stat(filepath.Join(snap, "store.db-wal")); !os.IsNotExist(err) {
		t.Fatalf("missing wal was invented: %v", err)
	}
}

func TestExcludedKey(t *testing.T) {
	for _, key := range []string{
		"cursorAuth", "cursorAuth/accessToken", "CursorAuth/refreshToken",
		"accessToken", "AccessToken", "refresh_token", "workosCursorSessionToken",
	} {
		if !excludedKey(key) {
			t.Fatalf("kept %s", key)
		}
	}
	for _, key := range []string{"name", "cursorAuthExtra", "cursor/auth", "accessTokenExtra", "0"} {
		if excludedKey(key) {
			t.Fatalf("dropped %s", key)
		}
	}
}

func TestScrubKeepsKeyOrder(t *testing.T) {
	raw := []byte(`{"name":"kept-title","accessToken":"sekret-token","n":1767396459642,"nested":{"refreshToken":"sekret-refresh","ok":true}}`)
	got, ok := scrubIfJSON(raw)
	if !ok {
		t.Fatal("not json")
	}
	want := `{"name":"kept-title","n":1767396459642,"nested":{"ok":true}}`
	if string(got) != want {
		t.Fatalf("scrub %s", got)
	}
	var doc map[string]any
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatal(err)
	}
}

func TestWatchNoticesWAL(t *testing.T) {
	root := t.TempDir()
	rel := "chats/ab12/sid-1/store.db-wal"
	path := filepath.Join(root, "chats", "ab12", "sid-1", "store.db-wal")
	mustWrite(t, filepath.Join(root, "chats", "ab12", "sid-1", "store.db"), "db")
	mustWrite(t, path, "wal")
	a := Adapter{}
	w, ch := startLayout(t, root, a.WatchDir(), a.Match, true)
	waitTracked(t, w, "chats/ab12/sid-1/store.db")
	waitTracked(t, w, rel)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("more"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	c := waitChange(t, ch, rel)
	if c.Op != watch.OpAppend && c.Op != watch.OpReplace {
		t.Fatalf("wal change %+v", c)
	}
}

func seedLive(t *testing.T, path string) (string, error) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return "", err
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		return "", err
	}
	if _, err := db.Exec(`PRAGMA wal_autocheckpoint=0`); err != nil {
		return "", err
	}
	if _, err := db.Exec(`CREATE TABLE blobs (id TEXT PRIMARY KEY, data BLOB)`); err != nil {
		return "", err
	}
	if _, err := db.Exec(`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		return "", err
	}
	meta := `{"name":"kept-title","accessToken":"sekret-token","createdAt":1767396459642,"cursorAuth":{"refreshToken":"sekret-refresh"}}`
	if _, err := db.Exec(`INSERT INTO meta (key, value) VALUES ('0', ?), ('cursorAuth/accessToken', 'sekret-plain')`, hex.EncodeToString([]byte(meta))); err != nil {
		return "", err
	}
	msg := `{"role":"user","content":"hello from cli","accessToken":"sekret-blob","note":"the field accessToken is a name"}`
	if _, err := db.Exec(`INSERT INTO blobs (id, data) VALUES ('blob-1', ?), ('cursorAuth/session', 'sekret-plain')`, msg); err != nil {
		return "", err
	}
	if _, err := db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return "", err
	}
	const walID = "blob-wal"
	if _, err := db.Exec(`INSERT INTO blobs (id, data) VALUES (?, '{"text":"from-wal"}')`, walID); err != nil {
		return "", err
	}
	if _, err := os.Stat(path + "-wal"); err != nil {
		return "", err
	}
	if _, err := os.Stat(path + "-shm"); err != nil {
		return "", err
	}
	return "from-wal", nil
}

func seedClosed(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE blobs (id TEXT PRIMARY KEY, data BLOB)`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO meta (key, value) VALUES ('0', '{"name":"closed"}')`)
	return err
}

func hashes(t *testing.T, dbPath string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		p := dbPath + suffix
		b, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		out[suffix] = string(b)
	}
	return out
}

func bytesContainAuth(b []byte) bool {
	s := string(b)
	return strings.Contains(s, "cursorAuth") || strings.Contains(s, "sekret-token") || strings.Contains(s, "sekret-refresh") || strings.Contains(s, "sekret-blob") || strings.Contains(s, "sekret-plain")
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func startLayout(t *testing.T, root, dir string, match func(string) (string, bool), poll bool) (*watch.Watcher, <-chan watch.Change) {
	t.Helper()
	ch := make(chan watch.Change, 16)
	w := &watch.Watcher{
		Root:         root,
		ForcePoll:    poll,
		Debounce:     30 * time.Millisecond,
		PollInterval: 20 * time.Millisecond,
		Layout:       watch.Layout{Dir: dir, Match: match},
		OnChange:     func(c watch.Change) { ch <- c },
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errCh := make(chan error, 1)
	go func() { errCh <- w.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-errCh:
			if err != nil {
				t.Errorf("run: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Errorf("run did not return")
		}
	})
	rctx, cancelReady := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelReady()
	if err := w.WaitReady(rctx); err != nil {
		t.Fatal(err)
	}
	return w, ch
}

func waitTracked(t *testing.T, w *watch.Watcher, rel string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, got := range w.Tracked() {
			if got == rel {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("never tracked %s; have %v", rel, w.Tracked())
}

func waitChange(t *testing.T, ch <-chan watch.Change, rel string) watch.Change {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case c := <-ch:
			if c.RelPath == rel {
				return c
			}
		case <-deadline:
			t.Fatalf("timeout waiting for %s", rel)
			return watch.Change{}
		}
	}
}
