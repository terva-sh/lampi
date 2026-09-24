package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/adapter/cursor"
	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/cas"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/upload"

	_ "modernc.org/sqlite"
)

// TestCursorWorkspaceEnrichmentPass is the dual-database fixture for
// Cursor 1b. Both databases are synthetic sqlite files under a temp
// directory. The test does not open a Cursor install.
func TestCursorWorkspaceEnrichmentPass(t *testing.T) {
	const (
		userText     = "allow-user-bubble"
		assistant    = "allow-assistant-bubble"
		otherText    = "other-workspace-bubble"
		decoyText    = "decoy-workspace-list-bubble"
		prefixText   = "prefix-must-not-match"
		agentText    = "agent-kv-not-merged"
		contentText  = "content-blob-not-merged"
		encOpaque    = "enc-opaque-value"
		cipherOpaque = "cipher-opaque-value"
		sealedOpaque = "sealed-opaque-value"
		wrongSecret  = "wrong-kind-secret"
	)

	home := t.TempDir()
	global := filepath.Join(home, "User", "globalStorage", "state.vscdb")
	allowDir := filepath.Join(home, "User", "workspaceStorage", "ws-allow")
	otherDir := filepath.Join(home, "User", "workspaceStorage", "ws-other")
	headers := func(ids ...string) string {
		t.Helper()
		all := make([]map[string]string, 0, len(ids))
		for _, id := range ids {
			all = append(all, map[string]string{"composerId": id})
		}
		b, err := json.Marshal(map[string]any{"allComposers": all})
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	writeEnrichDB(t, filepath.Join(allowDir, "state.vscdb"), []enrichKV{
		{"cursorAuth/accessToken", "sekret-token"},
		{"composer.composerHeaders", headers("comp-allow")},
		{"composer.composerData", headers("comp-decoy")},
	}, nil)
	writeEnrichDB(t, filepath.Join(otherDir, "state.vscdb"), []enrichKV{
		{"composer.composerHeaders", headers("comp-other")},
	}, nil)
	if err := os.WriteFile(filepath.Join(allowDir, "workspace.json"), []byte(`{"folder":"file:///work/app"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherDir, "workspace.json"), []byte(`{"folder":"file:///work/other"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeEnrichDB(t, global, []enrichKV{
		{"cursorAuth/refreshToken", "sekret-refresh"},
		{"composer.composerHeaders", headers("comp-other")},
		{"composer.composerData", headers("comp-allow", "comp-other", "comp-decoy")},
	}, []enrichKV{
		{"cursorAuth/accessToken", "sekret-token"},
		{"bubbleId:comp-allow:user", `{"type":1,"rawText":"` + userText + `","encrypted_content":"` + encOpaque + `"}`},
		{"bubbleId:comp-allow:assistant", `{"type":2,"text":"` + assistant + `","cipher_text":"` + cipherOpaque + `","sealed_payload":"` + sealedOpaque + `"}`},
		{"composerData:comp-allow", `{"name":"allow-thread","fullConversationHeadersOnly":[{"bubbleId":"user","type":1},{"bubbleId":"assistant","type":2}]}`},
		{"bubbleId:comp-other:user", `{"type":1,"rawText":"` + otherText + `"}`},
		{"composerData:comp-other", `{"name":"other-thread"}`},
		{"bubbleId:comp-decoy:user", `{"type":1,"rawText":"` + decoyText + `"}`},
		{"composerData:comp-decoy", `{"name":"decoy-thread"}`},
		{"bubbleId:comp-allow-extra:user", `{"type":1,"rawText":"` + prefixText + `"}`},
		{"agentKv:comp-allow", `{"text":"` + agentText + `"}`},
		{"composer.content.abc", `{"body":"` + contentText + `"}`},
	})
	before := map[string]string{
		global:                                 osRead(t, global),
		filepath.Join(allowDir, "state.vscdb"): osRead(t, filepath.Join(allowDir, "state.vscdb")),
	}

	data := t.TempDir()
	lake, err := api.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lake.Close() })
	lake.Allow("tok")
	lake.Allow("sekret")
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)

	opt := upload.Options{
		ServerURL:  srv.URL,
		Token:      "tok",
		MachineID:  "machine-1",
		StateDir:   t.TempDir(),
		Client:     srv.Client(),
		CursorHome: home,
		Projects:   config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: "/work/app"}}},
	}
	res, err := upload.Sync(context.Background(), opt)
	if err == nil || res.Uploaded != 1 || res.Manifests != 1 || res.Refused != 2 {
		t.Fatalf("sync %+v err=%v", res, err)
	}
	if !strings.Contains(err.Error(), "User/globalStorage/state.json") || !strings.Contains(err.Error(), "empty cwd") || !strings.Contains(err.Error(), "refused by design") {
		t.Fatalf("global was uploaded or the refusal omitted the reason: %v", err)
	}
	if !strings.Contains(err.Error(), "User/workspaceStorage/ws-other/state.json") {
		t.Fatalf("other workspace was not refused: %v", err)
	}
	if strings.Contains(err.Error(), "User/workspaceStorage/ws-allow/state.json") {
		t.Fatalf("allowlisted workspace was refused: %v", err)
	}
	for _, forbidden := range []string{"sekret-token", "sekret-refresh", otherText, decoyText, userText} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("refusal leaked %s", forbidden)
		}
	}
	for path, sum := range before {
		if osRead(t, path) != sum {
			t.Fatalf("live database changed: %s", path)
		}
	}

	ctx := context.Background()
	uid, arts, ok, err := lake.Catalog.Current(ctx, protocol.HarnessCursor, "workspace/ws-allow")
	if err != nil || !ok || uid == "" || len(arts) != 1 {
		t.Fatalf("catalog ok=%v err=%v arts=%d", ok, err, len(arts))
	}
	if arts[0].Kind != protocol.KindCursorStateJSON {
		t.Fatalf("kind %s", arts[0].Kind)
	}
	if _, _, ok, err := lake.Catalog.Current(ctx, protocol.HarnessCursor, "global"); err != nil || ok {
		t.Fatalf("global session uploaded ok=%v err=%v", ok, err)
	}
	if _, _, ok, err := lake.Catalog.Current(ctx, protocol.HarnessCursor, "workspace/ws-other"); err != nil || ok {
		t.Fatalf("other workspace uploaded ok=%v err=%v", ok, err)
	}
	raw, err := lake.CAS.Read(arts[0].SHA256)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(userText)) || !bytes.Contains(raw, []byte(assistant)) {
		t.Fatalf("stored export dropped workspace bubbles: %s", raw)
	}
	for _, forbidden := range []string{
		"sekret-token", "sekret-refresh", "cursorAuth",
		otherText, decoyText, prefixText, agentText, contentText,
	} {
		if bytes.Contains(raw, []byte(forbidden)) {
			t.Fatalf("stored export contains %s", forbidden)
		}
	}
	if !bytes.Contains(raw, []byte(`"harness_version":"`+cursor.Version+`"`)) || !bytes.Contains(raw, []byte(`"confidence":"low"`)) {
		t.Fatalf("pin: %s", raw)
	}
	sessions, err := lake.Catalog.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var saw bool
	for _, sess := range sessions {
		if sess.NativeID != "workspace/ws-allow" {
			continue
		}
		saw = true
		if sess.Manifest.HarnessVersion != cursor.Version || sess.Manifest.NativeSessionID != "workspace/ws-allow" {
			t.Fatalf("manifest %+v", sess.Manifest)
		}
		if sess.Manifest.NativeSessionID == "global" {
			t.Fatal("workspace export used the global session id")
		}
	}
	if !saw {
		t.Fatal("workspace session missing from the catalog")
	}

	wrongBody := []byte("{\"message\":\"" + wrongSecret + "\"}\n")
	sum, _, err := cas.Hash(bytes.NewReader(wrongBody))
	if err != nil {
		t.Fatal(err)
	}
	putBlob(t, lake.Handler(), sum, wrongBody)
	wrongAck := postManifest(t, lake.Handler(), protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-1",
		Harness:         protocol.HarnessCursor,
		HarnessVersion:  cursor.Version,
		NativeSessionID: "workspace/ws-wrong",
		Project:         protocol.Project{CWD: "/work/app"},
		Artifacts: []protocol.Artifact{{
			Kind:    protocol.KindTranscriptJSONL,
			RelPath: "projects/x/sid.jsonl",
			Size:    int64(len(wrongBody)),
			SHA256:  sum,
		}},
	})
	if err := lake.WaitNormalized(ctx); err != nil {
		t.Fatal(err)
	}
	msg, ok, err := lake.Catalog.NormalizeError(ctx, wrongAck.SessionUID)
	if err != nil || !ok || !strings.Contains(msg, "no cursor_state_json artifact") {
		t.Fatalf("normalize_error %q ok=%v err=%v", msg, ok, err)
	}
	if strings.Contains(msg, wrongSecret) {
		t.Fatalf("normalize_error includes the body: %s", msg)
	}
	goodMsg, goodOK, err := lake.Catalog.NormalizeError(ctx, uid)
	if err != nil || !goodOK || goodMsg != "" {
		t.Fatalf("workspace normalize_error %q ok=%v err=%v", goodMsg, goodOK, err)
	}

	if err := lake.Close(); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	eventsPath := filepath.Join(data, "events.jsonl")
	env := Env{Stdout: &bytes.Buffer{}, Stderr: &stderr}
	if err := Run([]string{"export", "--data", data, "--out", eventsPath, "--format", "events"}, env); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "normalize_error") || !strings.Contains(stderr.String(), wrongAck.SessionUID) {
		t.Fatalf("stderr: %s", stderr.String())
	}
	if strings.Contains(stderr.String(), wrongSecret) || strings.Contains(stderr.String(), "sekret-token") {
		t.Fatalf("stderr leaked a body: %s", stderr.String())
	}
	if queryContent(t, eventsPath, userText) != userText {
		t.Fatal("events export missed the type 1 bubble")
	}
	if queryContent(t, eventsPath, assistant) != assistant {
		t.Fatal("events export missed the type 2 bubble")
	}
	eventsRaw, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(eventsRaw, []byte(wrongSecret)) || bytes.Contains(eventsRaw, []byte(otherText)) || bytes.Contains(eventsRaw, []byte(decoyText)) || bytes.Contains(eventsRaw, []byte(prefixText)) {
		t.Fatalf("events export contains a foreign composer or the wrong kind:\n%s", eventsRaw)
	}
	var sawUser, sawAssistant bool
	for _, line := range splitLines(eventsRaw) {
		var ev struct {
			SessionID      string          `json:"session_id"`
			EventType      string          `json:"event_type"`
			ContentText    *string         `json:"content_text"`
			HarnessVersion *string         `json:"harness_version"`
			Extra          json.RawMessage `json:"extra"`
		}
		if err := json.Unmarshal(line, &ev); err != nil {
			t.Fatal(err)
		}
		if ev.SessionID != "cursor:workspace/ws-allow" {
			t.Fatalf("session_id %s", ev.SessionID)
		}
		if ev.SessionID == "cursor:global" {
			t.Fatal("global session id on a workspace export")
		}
		if ev.HarnessVersion == nil || *ev.HarnessVersion != cursor.Version {
			t.Fatalf("harness_version %v", ev.HarnessVersion)
		}
		if ev.ContentText != nil {
			for _, forbidden := range []string{encOpaque, cipherOpaque, sealedOpaque, "sekret-token", otherText, decoyText} {
				if strings.Contains(*ev.ContentText, forbidden) {
					t.Fatalf("content_text %q contains %s", *ev.ContentText, forbidden)
				}
			}
		}
		switch {
		case ev.ContentText != nil && *ev.ContentText == userText:
			sawUser = true
			if !bytes.Contains(ev.Extra, []byte(encOpaque)) {
				t.Fatalf("encrypted field dropped from extra: %s", ev.Extra)
			}
		case ev.ContentText != nil && *ev.ContentText == assistant:
			sawAssistant = true
			if !bytes.Contains(ev.Extra, []byte(cipherOpaque)) || !bytes.Contains(ev.Extra, []byte(sealedOpaque)) {
				t.Fatalf("opaque fields dropped from extra: %s", ev.Extra)
			}
		}
	}
	if !sawUser || !sawAssistant {
		t.Fatalf("user=%v assistant=%v\n%s", sawUser, sawAssistant, eventsRaw)
	}

	aloneHome := t.TempDir()
	writeEnrichDB(t, filepath.Join(aloneHome, "User", "globalStorage", "state.vscdb"), []enrichKV{
		{"cursorAuth/accessToken", "sekret-token"},
		{"composer.composerHeaders", headers("comp-allow", "comp-other")},
		{"composer.composerData", headers("comp-allow", "comp-other", "comp-decoy")},
	}, []enrichKV{
		{"bubbleId:comp-allow:user", `{"type":1,"rawText":"` + userText + `"}`},
		{"bubbleId:comp-other:user", `{"type":1,"rawText":"` + otherText + `"}`},
		{"bubbleId:comp-decoy:user", `{"type":1,"rawText":"` + decoyText + `"}`},
	})
	aloneData := t.TempDir()
	aloneLake, err := api.Open(aloneData)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = aloneLake.Close() })
	aloneLake.Allow("tok")
	aloneSrv := httptest.NewServer(aloneLake.Handler())
	t.Cleanup(aloneSrv.Close)
	aloneOpt := opt
	aloneOpt.ServerURL = aloneSrv.URL
	aloneOpt.Client = aloneSrv.Client()
	aloneOpt.CursorHome = aloneHome
	aloneOpt.StateDir = t.TempDir()
	aloneRes, err := upload.Sync(context.Background(), aloneOpt)
	if err == nil || aloneRes.Uploaded != 0 || aloneRes.Manifests != 0 || aloneRes.Refused != 1 {
		t.Fatalf("global alone %+v err=%v", aloneRes, err)
	}
	if !strings.Contains(err.Error(), "User/globalStorage/state.json") || !strings.Contains(err.Error(), "refused by design") {
		t.Fatalf("global alone refusal: %v", err)
	}
	if strings.Contains(err.Error(), userText) || strings.Contains(err.Error(), "sekret-token") {
		t.Fatalf("global alone leaked a body: %v", err)
	}
	if _, _, ok, err := aloneLake.Catalog.Current(context.Background(), protocol.HarnessCursor, "global"); err != nil || ok {
		t.Fatalf("global alone uploaded ok=%v err=%v", ok, err)
	}
	if enrichBlobCount(t, filepath.Join(aloneData, "cas")) != 0 {
		t.Fatal("global alone left a blob in the CAS")
	}
}

type enrichKV struct {
	key, val string
}

func writeEnrichDB(t *testing.T, path string, items, disk []enrichKV) {
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

func osRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func enrichBlobCount(t *testing.T, root string) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(root, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !d.IsDir() {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}
