package cursor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/adapter/sqlitesnap"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/redact"
	"terva.sh/lampi/internal/watch"

	_ "modernc.org/sqlite"
)

func TestHome(t *testing.T) {
	got, err := userDataDir("linux", func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			return "/tmp/xdg"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join("/tmp/xdg", "Cursor") {
		t.Fatalf("xdg %s", got)
	}

	got, err = userDataDir("linux", func(k string) string {
		if k == "HOME" {
			return "/home/drew"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join("/home/drew", ".config", "Cursor") {
		t.Fatalf("linux default %s", got)
	}

	got, err = userDataDir("darwin", func(k string) string {
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
	if got != filepath.Join("/Users/drew", "Library", "Application Support", "Cursor") {
		t.Fatalf("darwin %s", got)
	}

	got, err = userDataDir("windows", func(k string) string {
		if k == "APPDATA" {
			return `C:\Users\drew\AppData\Roaming`
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(`C:\Users\drew\AppData\Roaming`, "Cursor") {
		t.Fatalf("appdata %s", got)
	}

	got, err = userDataDir("windows", func(k string) string {
		if k == "USERPROFILE" {
			return `C:\Users\drew`
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(`C:\Users\drew`, "AppData", "Roaming", "Cursor") {
		t.Fatalf("userprofile %s", got)
	}

	if _, err := userDataDir("linux", func(string) string { return "" }); err == nil {
		t.Fatal("missing home should fail")
	}
}

func TestFileURI(t *testing.T) {
	got, ok := fileURIPath("linux", "file:///work/my%20app")
	if !ok || got != "/work/my app" {
		t.Fatalf("decoded %q ok=%v", got, ok)
	}
	got, ok = fileURIPath("windows", "file:///C:/Users/drew/proj")
	if !ok || got != filepath.FromSlash("C:/Users/drew/proj") {
		t.Fatalf("windows %q ok=%v", got, ok)
	}
	if _, ok := fileURIPath("linux", "vscode-remote://ssh-remote+host/work/app"); ok {
		t.Fatal("remote uri was treated as a local path")
	}
	if _, ok := fileURIPath("linux", "file://otherhost/work/app"); ok {
		t.Fatal("non-local file host was accepted")
	}
}

func TestDiscoverSkipsSidecarsAndSecretsPath(t *testing.T) {
	root := t.TempDir()
	global := filepath.Join(root, "User", "globalStorage")
	ws := filepath.Join(root, "User", "workspaceStorage", "abc")
	mustWrite(t, filepath.Join(global, "state.vscdb"), "db")
	mustWrite(t, filepath.Join(global, "state.vscdb-wal"), "wal")
	mustWrite(t, filepath.Join(global, "state.vscdb-shm"), "shm")
	mustWrite(t, filepath.Join(global, "state.vscdb.backup"), "backup")
	mustWrite(t, filepath.Join(ws, "state.vscdb"), "ws")
	mustWrite(t, filepath.Join(ws, "state.vscdb-wal"), "wal")
	mustWrite(t, filepath.Join(root, "User", "other", "state.vscdb"), "no")
	mustWrite(t, filepath.Join(root, "chats", "ab12", "sid-1", "store.db"), "cli")

	refs, err := Adapter{}.Discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range refs {
		got[r.RelPath] = true
		if r.Kind != protocol.KindCursorStateJSON {
			t.Fatalf("kind %s", r.Kind)
		}
	}
	for _, rel := range []string{
		"User/globalStorage/state.vscdb",
		"User/workspaceStorage/abc/state.vscdb",
	} {
		if !got[rel] {
			t.Fatalf("missing %s in %v", rel, got)
		}
	}
	for _, rel := range []string{
		"User/globalStorage/state.vscdb-wal",
		"User/globalStorage/state.vscdb-shm",
		"User/globalStorage/state.vscdb.backup",
		"User/workspaceStorage/abc/state.vscdb-wal",
		"User/other/state.vscdb",
		"chats/ab12/sid-1/store.db",
	} {
		if got[rel] {
			t.Fatalf("listed %s", rel)
		}
	}
	if _, err := (Adapter{}).Discover(context.Background(), filepath.Join(root, "missing")); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotFiltersAuthAndReadsWAL(t *testing.T) {
	root := t.TempDir()
	global := filepath.Join(root, "User", "globalStorage", "state.vscdb")
	wsDir := filepath.Join(root, "User", "workspaceStorage", "ws1")
	ws := filepath.Join(wsDir, "state.vscdb")
	keep, err := seedLive(t, global)
	if err != nil {
		t.Fatal(err)
	}
	if err := seedClosed(ws); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "workspace.json"), []byte(`{"folder":"file:///work/my%20app"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	before := hashes(t, global)
	body, err := exportDatabase(context.Background(), global, globalRel, "global")
	if err != nil {
		t.Fatal(err)
	}
	after := hashes(t, global)
	for name, sum := range before {
		if after[name] != sum {
			t.Fatalf("live %s changed", name)
		}
	}
	text := string(body)
	for _, secret := range []string{"sekret-token", "sekret-refresh", "cursorAuth"} {
		if strings.Contains(text, secret) {
			t.Fatalf("export contains %s: %s", secret, text)
		}
	}
	if !strings.Contains(text, "composer.composerData") || !strings.Contains(text, keep) || !strings.Contains(text, `"wal":true`) {
		t.Fatalf("export dropped a kept row: %s", text)
	}
	if !strings.Contains(text, `"harness_version":"`+Version+`"`) || !strings.Contains(text, `"confidence":"low"`) {
		t.Fatalf("pin: %s", text)
	}
	var doc document
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.CursorDiskKV == nil {
		t.Fatal("cursorDiskKV was omitted")
	}
	for _, rows := range [][]row{doc.ItemTable, *doc.CursorDiskKV} {
		for _, row := range rows {
			if excludedKey(row.Key) {
				t.Fatalf("kept %s", row.Key)
			}
		}
	}

	// The row inserted after the checkpoint is not in the main file alone.
	alone := filepath.Join(t.TempDir(), "state.vscdb")
	mainOnly, err := os.ReadFile(global)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(alone, mainOnly, 0o600); err != nil {
		t.Fatal(err)
	}
	aloneBody, err := exportDatabase(context.Background(), alone, globalRel, "global")
	if err == nil && strings.Contains(string(aloneBody), keep) {
		t.Fatal("db copied without its wal still contained the wal-only row")
	}

	b, err := Manifests(root, "machine-1")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Cleanup()
	if len(b.Manifests) != 2 {
		t.Fatalf("manifests %d", len(b.Manifests))
	}
	byID := map[string]protocol.Manifest{}
	for _, m := range b.Manifests {
		byID[m.NativeSessionID] = m
		if m.Harness != protocol.HarnessCursor || m.HarnessVersion != Version || m.CaptureProtocol != 1 {
			t.Fatalf("header %+v", m)
		}
		if len(m.Artifacts) != 1 || m.Artifacts[0].Kind != protocol.KindCursorStateJSON {
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
	g := byID["global"]
	if g.Project.CWD != "" || g.Artifacts[0].RelPath != "User/globalStorage/state.json" {
		t.Fatalf("global %+v", g)
	}
	w := byID["workspace/ws1"]
	if w.Project.CWD != "/work/my app" {
		t.Fatalf("cwd %q", w.Project.CWD)
	}
	if w.Artifacts[0].RelPath != "User/workspaceStorage/ws1/state.json" {
		t.Fatalf("rel %s", w.Artifacts[0].RelPath)
	}

	rc, err := Adapter{}.ReadSlice(context.Background(), global, 0)
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
	if _, err := (Adapter{}).ReadSlice(context.Background(), global+"-wal", 0); err == nil {
		t.Fatal("wal sidecar was readable")
	}
}

func TestWorkspaceMergesComposerHeaders(t *testing.T) {
	root := t.TempDir()
	global := filepath.Join(root, "User", "globalStorage", "state.vscdb")
	allowDir := filepath.Join(root, "User", "workspaceStorage", "ws-allow")
	allow := filepath.Join(allowDir, "state.vscdb")
	otherDir := filepath.Join(root, "User", "workspaceStorage", "ws-other")
	other := filepath.Join(otherDir, "state.vscdb")
	mustWrite(t, filepath.Join(allowDir, "workspace.json"), `{"folder":"file:///work/app"}`)
	mustWrite(t, filepath.Join(otherDir, "workspace.json"), `{"folder":"file:///work/other"}`)

	headers := func(ids ...string) string {
		t.Helper()
		var all []map[string]string
		for _, id := range ids {
			all = append(all, map[string]string{"composerId": id})
		}
		b, err := json.Marshal(map[string]any{"allComposers": all})
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	writeStateDB(t, allow, []stateKV{
		{"cursorAuth/accessToken", "sekret-token"},
		{"composer.composerHeaders", headers("comp-allow")},
		{"composer.composerData", headers("comp-decoy")},
	}, nil)
	writeStateDB(t, other, []stateKV{
		{"composer.composerHeaders", headers("comp-other")},
	}, nil)
	writeStateDB(t, global, []stateKV{
		{"cursorAuth/refreshToken", "sekret-refresh"},
		// A global copy of the headers key is not the registry. It names
		// the other workspace so a reader that uses this row instead of
		// the workspace key pulls the wrong composer.
		{"composer.composerHeaders", headers("comp-other")},
		{"composer.composerData", headers("comp-allow", "comp-other", "comp-decoy")},
	}, []stateKV{
		{"cursorAuth/accessToken", "sekret-token"},
		{"bubbleId:comp-allow:user", `{"type":1,"rawText":"allow-user-bubble","encrypted_content":"enc-opaque-value"}`},
		{"bubbleId:comp-allow:assistant", `{"type":2,"text":"allow-assistant-bubble","cipher_text":"cipher-opaque-value","sealed_payload":"sealed-opaque-value"}`},
		{"composerData:comp-allow", `{"name":"allow-thread","fullConversationHeadersOnly":[{"bubbleId":"user","type":1},{"bubbleId":"assistant","type":2}]}`},
		{"bubbleId:comp-other:user", `{"type":1,"rawText":"other-workspace-bubble"}`},
		{"composerData:comp-other", `{"name":"other-thread"}`},
		{"bubbleId:comp-decoy:user", `{"type":1,"rawText":"decoy-workspace-list-bubble"}`},
		{"composerData:comp-decoy", `{"name":"decoy-thread"}`},
		{"bubbleId:comp-allow-extra:user", `{"type":1,"rawText":"prefix-must-not-match"}`},
		{"agentKv:comp-allow", `{"text":"agent-kv-not-merged"}`},
		{"composer.content.abc", `{"body":"content-blob-not-merged"}`},
	})

	beforeG := hashes(t, global)
	beforeA := hashes(t, allow)
	b, err := Manifests(root, "machine-1")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Cleanup()
	for name, sum := range beforeG {
		if hashes(t, global)[name] != sum {
			t.Fatalf("live global %s changed", name)
		}
	}
	for name, sum := range beforeA {
		if hashes(t, allow)[name] != sum {
			t.Fatalf("live workspace %s changed", name)
		}
	}

	byID := map[string]protocol.Manifest{}
	for _, m := range b.Manifests {
		byID[m.NativeSessionID] = m
		if m.HarnessVersion != Version {
			t.Fatalf("version %s", m.HarnessVersion)
		}
	}
	w, ok := byID["workspace/ws-allow"]
	if !ok {
		t.Fatalf("sessions %v", byID)
	}
	if w.NativeSessionID == "global" || w.Project.CWD != "/work/app" {
		t.Fatalf("workspace session %+v", w)
	}
	raw, err := os.ReadFile(b.Paths[w.Artifacts[0].RelPath])
	if err != nil {
		t.Fatal(err)
	}
	var doc document
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.HarnessVersion != Version || doc.Confidence != "low" || doc.Scope != "workspace" {
		t.Fatalf("pin %+v", doc)
	}
	if doc.CursorDiskKV == nil {
		t.Fatal("merged cursor_disk_kv missing")
	}
	got := map[string]bool{}
	for _, row := range *doc.CursorDiskKV {
		got[row.Key] = true
		if excludedKey(row.Key) {
			t.Fatalf("kept %s", row.Key)
		}
	}
	for _, key := range []string{"bubbleId:comp-allow:user", "bubbleId:comp-allow:assistant", "composerData:comp-allow"} {
		if !got[key] {
			t.Fatalf("missing %s in %v", key, got)
		}
	}
	for _, key := range []string{
		"bubbleId:comp-other:user",
		"composerData:comp-other",
		"bubbleId:comp-decoy:user",
		"composerData:comp-decoy",
		"bubbleId:comp-allow-extra:user",
		"agentKv:comp-allow",
		"composer.content.abc",
	} {
		if got[key] {
			t.Fatalf("merged %s", key)
		}
	}
	text := string(raw)
	for _, forbidden := range []string{
		"sekret-token", "sekret-refresh", "cursorAuth",
		"other-workspace-bubble", "decoy-workspace-list-bubble",
		"prefix-must-not-match", "agent-kv-not-merged", "content-blob-not-merged",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("export contains %s", forbidden)
		}
	}
	if !strings.Contains(text, "allow-user-bubble") || !strings.Contains(text, "allow-assistant-bubble") {
		t.Fatalf("export dropped the workspace bubbles: %s", text)
	}
	g := byID["global"]
	if g.Project.CWD != "" || g.NativeSessionID != "global" {
		t.Fatalf("global %+v", g)
	}

	// Headers with no global file still export. The workspace list does
	// not pull bubbles that are not there.
	alone := t.TempDir()
	aloneDB := filepath.Join(alone, "User", "workspaceStorage", "ws-solo", "state.vscdb")
	writeStateDB(t, aloneDB, []stateKV{
		{"composer.composerHeaders", headers("comp-allow")},
		{"composer.composerData", headers("comp-decoy")},
	}, nil)
	body, err := exportDatabase(context.Background(), aloneDB, "User/workspaceStorage/ws-solo/state.vscdb", "workspace")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "allow-user-bubble") || strings.Contains(string(body), "cursor_disk_kv") {
		t.Fatalf("missing global invented disk rows: %s", body)
	}
}

type stateKV struct {
	key, val string
}

func writeStateDB(t *testing.T, path string, items, disk []stateKV) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE ItemTable (key TEXT UNIQUE ON CONFLICT REPLACE, value BLOB)`); err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if _, err := db.Exec(`INSERT INTO ItemTable (key, value) VALUES (?, ?)`, item.key, item.val); err != nil {
			t.Fatal(err)
		}
	}
	if disk == nil {
		return
	}
	if _, err := db.Exec(`CREATE TABLE cursorDiskKV (key TEXT UNIQUE ON CONFLICT REPLACE, value BLOB)`); err != nil {
		t.Fatal(err)
	}
	for _, item := range disk {
		if _, err := db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES (?, ?)`, item.key, item.val); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEncodedValuesAreScannedRaw(t *testing.T) {
	pat := "ghp_" + strings.Repeat("Q", 36)
	raw, hidden, err := encodeValue([]byte("\xff\xfe" + pat + "\x00"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), pat) || !strings.Contains(string(raw), `"base64"`) {
		t.Fatalf("value %s", raw)
	}
	if hidden.Hits != 1 || len(hidden.Rules) != 1 || hidden.Rules[0] != "github-pat" {
		t.Fatalf("scan %+v", hidden)
	}
	if _, clean, _ := encodeValue([]byte("text " + pat)); clean.Hits != 0 {
		t.Fatalf("utf-8 text is scanned in the export, not here: %+v", clean)
	}

	// A global row for a composer this workspace does not name is not
	// in the export, so its scan does not count.
	mine := row{Key: "bubbleId:c1:b1", hidden: hidden}
	theirs := row{Key: "bubbleId:c2:b1", hidden: hidden}
	doc := document{ItemTable: []row{{Key: "k"}}}
	doc.CursorDiskKV = mergeDisk(nil, filterDisk([]row{mine, theirs}, map[string]struct{}{"c1": {}}))
	if got := doc.hidden(); got.Hits != 1 || len(got.Rules) != 1 {
		t.Fatalf("document scan %+v", got)
	}
}

func TestMissingItemTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.vscdb")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE other (k TEXT)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := exportDatabase(context.Background(), path, globalRel, "global"); err == nil {
		t.Fatal("database without ItemTable was accepted")
	}
}

// countSnapshots swaps takeSnapshot for one that counts copies by
// source path.
func countSnapshots(t *testing.T) map[string]int {
	t.Helper()
	counts := map[string]int{}
	takeSnapshot = func(ctx context.Context, src, prefix string) (*sqlitesnap.Snapshot, error) {
		counts[filepath.Clean(src)]++
		return sqlitesnap.Take(ctx, src, prefix)
	}
	t.Cleanup(func() { takeSnapshot = sqlitesnap.Take })
	return counts
}

// composerRoot is a user-data directory whose global database holds
// rows for three composers and whose three workspaces each name one.
func composerRoot(t *testing.T) (root, global string) {
	t.Helper()
	root = t.TempDir()
	global = filepath.Join(root, "User", "globalStorage", "state.vscdb")
	var disk []stateKV
	for _, ws := range []string{"a", "b", "c"} {
		dir := filepath.Join(root, "User", "workspaceStorage", "ws-"+ws)
		mustWrite(t, filepath.Join(dir, "workspace.json"), `{"folder":"file:///work/`+ws+`"}`)
		writeStateDB(t, filepath.Join(dir, "state.vscdb"), []stateKV{
			{"composer.composerHeaders", `{"allComposers":[{"composerId":"comp-` + ws + `"}]}`},
		}, nil)
		disk = append(disk,
			stateKV{"composerData:comp-" + ws, `{"name":"thread-` + ws + `"}`},
			stateKV{"bubbleId:comp-" + ws + ":b1", `{"text":"bubble-` + ws + `"}`},
		)
	}
	writeStateDB(t, global, []stateKV{{"composer.composerData", `{}`}}, disk)
	return root, global
}

func TestManifestsCopiesGlobalOnce(t *testing.T) {
	root, global := composerRoot(t)
	counts := countSnapshots(t)
	b, err := Manifests(root, "machine-1")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Cleanup()
	if len(b.Manifests) != 4 {
		t.Fatalf("manifests %d", len(b.Manifests))
	}
	if counts[global] != 1 {
		t.Fatalf("global copied %d times in one sync: %v", counts[global], counts)
	}
	for _, m := range b.Manifests {
		if m.NativeSessionID == "global" {
			continue
		}
		raw, err := os.ReadFile(b.Paths[m.Artifacts[0].RelPath])
		if err != nil {
			t.Fatal(err)
		}
		ws := strings.TrimPrefix(m.NativeSessionID, "workspace/ws-")
		if !strings.Contains(string(raw), "bubble-"+ws) {
			t.Fatalf("%s lost its bubble: %s", m.NativeSessionID, raw)
		}
		for _, other := range []string{"a", "b", "c"} {
			if other != ws && strings.Contains(string(raw), "bubble-"+other) {
				t.Fatalf("%s holds bubble-%s", m.NativeSessionID, other)
			}
		}
	}
}

func TestManifestsPermitSkipsRefusedSessions(t *testing.T) {
	root, global := composerRoot(t)
	counts := countSnapshots(t)
	var asked []string
	permit := func(m protocol.Manifest) bool {
		asked = append(asked, m.NativeSessionID)
		if len(m.Artifacts) != 1 || m.Artifacts[0].SHA256 != "" {
			t.Errorf("permit saw a built export: %+v", m.Artifacts)
		}
		return m.Project.CWD == "/work/a"
	}
	b, err := ManifestsPermit(root, "machine-1", permit)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Cleanup()
	if len(asked) != 4 || len(b.Manifests) != 4 {
		t.Fatalf("asked %v manifests %d", asked, len(b.Manifests))
	}
	if counts[global] != 1 {
		t.Fatalf("global copied %d times: %v", counts[global], counts)
	}
	for _, ws := range []string{"b", "c"} {
		if n := counts[filepath.Join(root, "User", "workspaceStorage", "ws-"+ws, "state.vscdb")]; n != 0 {
			t.Fatalf("refused ws-%s was copied %d times", ws, n)
		}
	}
	for _, m := range b.Manifests {
		a := m.Artifacts[0]
		if m.Project.CWD == "/work/a" {
			if a.SHA256 == "" || b.Paths[a.RelPath] == "" {
				t.Fatalf("permitted session not exported: %+v", a)
			}
			continue
		}
		if a.SHA256 != "" || a.TailSHA256 != "" || a.Size != 0 {
			t.Fatalf("refused %s carries an export: %+v", m.NativeSessionID, a)
		}
		if _, ok := b.Paths[a.RelPath]; ok {
			t.Fatalf("refused %s has a path", m.NativeSessionID)
		}
		if a.RelPath == "" || a.Kind != protocol.KindCursorStateJSON {
			t.Fatalf("refused %s lost its relpath: %+v", m.NativeSessionID, a)
		}
	}
	if len(b.Paths) != 1 {
		t.Fatalf("paths %v", b.Paths)
	}

	// No permitted session means no copy at all, the global one included.
	counts = countSnapshots(t)
	none, err := ManifestsPermit(root, "machine-1", func(protocol.Manifest) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	defer none.Cleanup()
	if len(counts) != 0 || len(none.Paths) != 0 || len(none.Manifests) != 4 {
		t.Fatalf("copies %v paths %v manifests %d", counts, none.Paths, len(none.Manifests))
	}
}

func TestComposerRowsSelectsInSQL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.vscdb")
	writeStateDB(t, path, []stateKV{{"k", "v"}}, []stateKV{
		{"composerData:c1", `{"mine":1}`},
		{"bubbleId:c1:b1", `{"mine":2}`},
		{"checkpointId:c1:k1", `{"mine":3}`},
		{"messageRequestContext:c1:m1", `{"mine":4}`},
		{"codeBlockDiff:c1:d1", `{"mine":5}`},
		{"cursorAuth/accessToken", "sekret-token"},
		{"bubbleId:c1:", `{"empty-tail":1}`},
		{"bubbleId:c1x:b1", `{"longer-id":1}`},
		{"bubbleId:c10:b1", `{"longer-id":2}`},
		{"bubbleId:c2:b1", `{"other":1}`},
		{"composerData:c1:extra", `{"not-composer-data":1}`},
		{"composerData:c2", `{"other":2}`},
		{"agentKv:c1", `{"unowned":1}`},
		{"BubbleId:c1:b1", `{"case":1}`},
	})
	snap, err := sqlitesnap.Take(context.Background(), path, "lampi-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Close()
	rows, err := composerRows(context.Background(), snap.DB, map[string]struct{}{"c1": {}})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range rows {
		got = append(got, r.Key)
	}
	want := []string{"bubbleId:c1:b1", "checkpointId:c1:k1", "codeBlockDiff:c1:d1", "composerData:c1", "messageRequestContext:c1:m1"}
	sorted := mergeDisk(nil, rows)
	if len(*sorted) != len(want) {
		t.Fatalf("rows %v want %v", got, want)
	}
	for i, r := range *sorted {
		if r.Key != want[i] {
			t.Fatalf("rows %v want %v", got, want)
		}
	}

	// A database without cursorDiskKV has no composer rows.
	bare := filepath.Join(t.TempDir(), "state.vscdb")
	writeStateDB(t, bare, []stateKV{{"k", "v"}}, nil)
	snap2, err := sqlitesnap.Take(context.Background(), bare, "lampi-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer snap2.Close()
	if rows, err := composerRows(context.Background(), snap2.DB, map[string]struct{}{"c1": {}}); err != nil || len(rows) != 0 {
		t.Fatalf("rows %v err %v", rows, err)
	}
}

type failingScanner struct{}

func (failingScanner) Scan([]byte) (redact.Result, error) {
	return redact.Result{}, errors.New("scanner broke")
}

// A value the ruleset cannot scan is not exported. The export fails
// and names the key.
func TestScanErrorFailsClosed(t *testing.T) {
	scanner = failingScanner{}
	t.Cleanup(func() { scanner = redact.Ruleset{} })
	if raw, _, err := encodeValue([]byte("\xff\xfe\x00")); err == nil || raw != nil {
		t.Fatalf("raw %s err %v", raw, err)
	}
	root := t.TempDir()
	path := filepath.Join(root, "User", "globalStorage", "state.vscdb")
	writeStateDB(t, path, []stateKV{{"binary.value", "\xff\xfe\x00"}}, nil)
	_, err := exportDatabase(context.Background(), path, globalRel, "global")
	if err == nil || !strings.Contains(err.Error(), `"binary.value"`) || !strings.Contains(err.Error(), "scanner broke") {
		t.Fatalf("err %v", err)
	}
	if _, err := Manifests(root, "machine-1"); err == nil {
		t.Fatal("manifests exported a value the ruleset could not scan")
	}
}

func TestExcludedKey(t *testing.T) {
	for _, key := range []string{"cursorAuth", "cursorAuth/accessToken", "CursorAuth/refreshToken"} {
		if !excludedKey(key) {
			t.Fatalf("kept %s", key)
		}
	}
	for _, key := range []string{"composer.composerData", "cursorAuthExtra", "cursor/auth"} {
		if excludedKey(key) {
			t.Fatalf("dropped %s", key)
		}
	}
}

func TestWatchNoticesWAL(t *testing.T) {
	root := t.TempDir()
	rel := "User/globalStorage/state.vscdb-wal"
	path := filepath.Join(root, "User", "globalStorage", "state.vscdb-wal")
	mustWrite(t, filepath.Join(root, "User", "globalStorage", "state.vscdb"), "db")
	mustWrite(t, path, "wal")
	a := Adapter{}
	w, ch := startLayout(t, root, a.WatchDir(), a.Match, true)
	waitTracked(t, w, "User/globalStorage/state.vscdb")
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

// seedLive inserts a checkpointed row and then a row that stays in the
// WAL. The connection stays open so closing it cannot checkpoint the
// second row into the main file before the caller copies the trio.
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
	if _, err := db.Exec(`CREATE TABLE ItemTable (key TEXT UNIQUE ON CONFLICT REPLACE, value BLOB)`); err != nil {
		return "", err
	}
	if _, err := db.Exec(`CREATE TABLE cursorDiskKV (key TEXT UNIQUE ON CONFLICT REPLACE, value BLOB)`); err != nil {
		return "", err
	}
	if _, err := db.Exec(`INSERT INTO ItemTable (key, value) VALUES ('cursorAuth/accessToken', 'sekret-token')`); err != nil {
		return "", err
	}
	if _, err := db.Exec(`INSERT INTO ItemTable (key, value) VALUES ('composer.composerData', '{"allComposers":[]}')`); err != nil {
		return "", err
	}
	if _, err := db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES ('cursorAuth/refreshToken', 'sekret-refresh')`); err != nil {
		return "", err
	}
	if _, err := db.Exec(`INSERT INTO cursorDiskKV (key, value) VALUES ('bubbleId:c1:b1', '{"text":"hello"}')`); err != nil {
		return "", err
	}
	if _, err := db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return "", err
	}
	const walKey = "aiService.prompts"
	if _, err := db.Exec(`INSERT INTO ItemTable (key, value) VALUES (?, '{"wal":true}')`, walKey); err != nil {
		return "", err
	}
	if _, err := os.Stat(path + "-wal"); err != nil {
		return "", err
	}
	if _, err := os.Stat(path + "-shm"); err != nil {
		return "", err
	}
	return walKey, nil
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
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE TABLE ItemTable (key TEXT UNIQUE ON CONFLICT REPLACE, value BLOB)`); err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO ItemTable (key, value) VALUES ('cursorAuth/accessToken', 'sekret-token'), ('workbench.panel.aichat', '{"ok":true}')`)
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
	return strings.Contains(s, "cursorAuth") || strings.Contains(s, "sekret-token") || strings.Contains(s, "sekret-refresh")
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
