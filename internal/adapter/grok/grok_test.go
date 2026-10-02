package grok_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/adapter/cursor"
	"terva.sh/lampi/internal/adapter/cursorcli"
	"terva.sh/lampi/internal/adapter/grok"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/testharness"
	"terva.sh/lampi/internal/watch"
)

const sessionUUIDFixture = "018f1a2b-3c4d-7e5f-8a9b-0c1d2e3f4a5b"

func TestHome(t *testing.T) {
	got, err := grok.Home(func(k string) string {
		if k == "GROK_HOME" {
			return "/tmp/grok-alt"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/grok-alt" {
		t.Fatalf("override %s", got)
	}

	got, err = grok.Home(func(k string) string {
		switch k {
		case "HOME":
			return "/home/drew"
		case "XDG_CONFIG_HOME":
			return "/xdg"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join("/home/drew", ".grok") {
		t.Fatalf("default %s, XDG must not move it", got)
	}

	if _, err := grok.Home(func(string) string { return "" }); err == nil {
		t.Fatal("missing home should fail")
	}
}

func TestDiscoverAndWatch(t *testing.T) {
	root := t.TempDir()
	cwd := "/work/demo"
	res, err := testharness.PlantGrok(root, cwd, []testharness.SessionSpec{{
		ID: sessionUUIDFixture, Prompt: "hello grok",
	}})
	if err != nil {
		t.Fatal(err)
	}
	// Unknown files and a non-UUID directory sit beside the session.
	// They are not sessions and they are not errors.
	mustWrite(t, filepath.Join(filepath.Dir(res.Files[0]), "chat_history.jsonl"), "{}\n")
	mustWrite(t, filepath.Join(filepath.Dir(res.Files[0]), "notes.txt"), "no")
	mustWrite(t, filepath.Join(root, "sessions", "not-a-uuid", "updates.jsonl"), "{}\n")
	mustWrite(t, filepath.Join(root, "history.jsonl"), "{}\n")

	refs, err := grok.Adapter{}.Discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range refs {
		got[r.RelPath] = r.Kind
	}
	enc := grok.EncodeCWDDirname(cwd)
	if enc != "%2Fwork%2Fdemo" {
		t.Fatalf("encoded cwd %s", enc)
	}
	transcript := "sessions/" + enc + "/" + sessionUUIDFixture + "/updates.jsonl"
	summary := "sessions/" + enc + "/" + sessionUUIDFixture + "/summary.json"
	if got[transcript] != protocol.KindTranscriptJSONL {
		t.Fatalf("transcript %v", got)
	}
	if got[summary] != protocol.KindSummaryJSON {
		t.Fatalf("summary %v", got)
	}
	if len(got) != 2 {
		t.Fatalf("discovered more than the session: %v", got)
	}

	empty, err := grok.Adapter{}.Discover(context.Background(), t.TempDir())
	if err != nil || len(empty) != 0 {
		t.Fatalf("missing sessions: %v %d", err, len(empty))
	}

	for _, poll := range []bool{false, true} {
		name := "fsnotify"
		if poll {
			name = "poll"
		}
		t.Run(name, func(t *testing.T) {
			w, ch := startLayout(t, root, grok.Adapter{}.WatchDir(), grok.Adapter{}.Match, poll)
			waitTracked(t, w, transcript)
			for _, tracked := range w.Tracked() {
				if strings.HasSuffix(tracked, "chat_history.jsonl") || strings.HasSuffix(tracked, "notes.txt") || strings.Contains(tracked, "not-a-uuid") {
					t.Fatalf("tracked %s", tracked)
				}
			}
			f, err := os.OpenFile(res.Files[0], os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteString(`{"type":"user","text":"more"}` + "\n"); err != nil {
				t.Fatal(err)
			}
			f.Close()
			c := waitChange(t, ch, transcript)
			if c.Op != watch.OpAppend || c.Offset == 0 {
				t.Fatalf("append %+v", c)
			}
		})
	}
}

func TestManifestPinsVersionAndSummary(t *testing.T) {
	if grok.Version != "1" {
		t.Fatalf("adapter version %q", grok.Version)
	}
	root := t.TempDir()
	cwd := "/work/demo"
	res, err := testharness.PlantGrok(root, cwd, []testharness.SessionSpec{{
		ID: sessionUUIDFixture, Prompt: "hello grok",
	}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := grok.Manifests(root, "machine-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Manifests) != 1 {
		t.Fatalf("manifests %d", len(b.Manifests))
	}
	m := b.Manifests[0]
	if m.Harness != protocol.HarnessGrok || m.HarnessVersion != grok.Version || m.CaptureProtocol != 1 {
		t.Fatalf("header %+v", m)
	}
	if m.NativeSessionID != sessionUUIDFixture {
		t.Fatalf("native id %s", m.NativeSessionID)
	}
	if m.Project.CWD != cwd || m.Project.CWDHash == "" {
		t.Fatalf("project %+v", m.Project)
	}
	if len(m.Artifacts) != 2 {
		t.Fatalf("artifacts %+v", m.Artifacts)
	}
	if m.Artifacts[0].Kind != protocol.KindTranscriptJSONL || !strings.HasSuffix(m.Artifacts[0].RelPath, "updates.jsonl") {
		t.Fatalf("head %+v", m.Artifacts[0])
	}
	if m.Artifacts[1].Kind != protocol.KindSummaryJSON || !strings.HasSuffix(m.Artifacts[1].RelPath, "summary.json") {
		t.Fatalf("companion %+v", m.Artifacts[1])
	}
	summaryBody, err := os.ReadFile(filepath.Join(filepath.Dir(res.Files[0]), "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(summaryBody, []byte(`"cwd":"`+cwd+`"`)) || !bytes.Contains(summaryBody, []byte(`"generated_title":"synthetic `+sessionUUIDFixture+`"`)) || !bytes.Contains(summaryBody, []byte(`"current_model_id":"grok-build"`)) {
		t.Fatalf("summary %s", summaryBody)
	}

	// A summary with no cwd recovers an absolute path from the directory name.
	summaryPath := filepath.Join(filepath.Dir(res.Files[0]), "summary.json")
	if err := os.WriteFile(summaryPath, []byte(`{"info":{"id":"`+sessionUUIDFixture+`"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err = grok.Manifests(root, "machine-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Manifests) != 1 || b.Manifests[0].Project.CWD != cwd {
		t.Fatalf("dirname cwd %+v", b.Manifests)
	}
}

func TestLongCWDComesFromSummaryAndCWDFile(t *testing.T) {
	root := t.TempDir()
	cwd := "/Users/test/" + strings.Repeat("中", 30)
	if !grok.UsesCWDFile(cwd) {
		t.Fatal("fixture cwd should exceed 255 encoded bytes")
	}
	res, err := testharness.PlantGrok(root, cwd, []testharness.SessionSpec{{
		ID: sessionUUIDFixture, Prompt: "long",
	}})
	if err != nil {
		t.Fatal(err)
	}
	enc := grok.EncodeCWDDirname(cwd)
	if len(enc) > 255 || strings.Contains(enc, "%") {
		t.Fatalf("dirname %s", enc)
	}
	b, err := grok.Manifests(root, "machine-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Manifests) != 1 || b.Manifests[0].Project.CWD != cwd {
		t.Fatalf("summary cwd %+v", b.Manifests)
	}

	// A summary with no cwd still recovers the path from .cwd.
	summaryPath := filepath.Join(filepath.Dir(res.Files[0]), "summary.json")
	if err := os.WriteFile(summaryPath, []byte(`{"info":{"id":"`+sessionUUIDFixture+`"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err = grok.Manifests(root, "machine-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Manifests) != 1 || b.Manifests[0].Project.CWD != cwd {
		t.Fatalf("fallback cwd %+v", b.Manifests)
	}
}

func TestUnknownAndRefusedPathsDoNotFail(t *testing.T) {
	root := t.TempDir()
	if _, err := testharness.PlantGrok(root, "work/demo", []testharness.SessionSpec{{ID: sessionUUIDFixture}}); err == nil || !strings.Contains(err.Error(), "cwd must be an absolute path") {
		t.Fatalf("relative cwd: %v", err)
	}
	if _, err := testharness.PlantGrok(root, "/work/demo", []testharness.SessionSpec{{ID: "sess-1"}}); err == nil || !strings.Contains(err.Error(), "UUID") {
		t.Fatalf("non-uuid: %v", err)
	}
	left, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("rejected plant wrote %d entries", len(left))
	}

	res, err := testharness.PlantGrok(root, "/work/demo", []testharness.SessionSpec{{
		ID: sessionUUIDFixture, Prompt: "ok", Extra: map[string]string{"leak": "should-not-appear-xyz"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(res.Files[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "should-not-appear-xyz") || strings.Contains(string(body), "leak") {
		t.Fatalf("extra key was written: %s", body)
	}
	mustWrite(t, filepath.Join(root, "sessions", "weird", "updates.jsonl"), "not-json{{{")
	mustWrite(t, filepath.Join(filepath.Dir(res.Files[0]), "plan.json"), "{")
	b, err := grok.Manifests(root, "machine-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Manifests) != 1 || b.Manifests[0].NativeSessionID != sessionUUIDFixture {
		t.Fatalf("manifests %+v skipped %v", b.Manifests, b.Skipped)
	}
	if len(b.Skipped) != 0 {
		t.Fatalf("unknown paths were skips: %v", b.Skipped)
	}
}

func TestRecordKeepsUnknownFieldsAndHasNoConfidence(t *testing.T) {
	if _, ok := reflect.TypeOf(grok.Record{}).FieldByName("Confidence"); ok {
		t.Fatal("JSONL record has a Confidence field")
	}
	line := []byte(`{"type":"user","confidence":"low","future_field":{"n":1}}`)
	rec, err := grok.ParseLine(line)
	if err != nil {
		t.Fatal(err)
	}
	if string(rec.Extra["confidence"]) != `"low"` {
		t.Fatalf("confidence was interpreted: %s", rec.Extra["confidence"])
	}
	if string(rec.Extra["future_field"]) != `{"n":1}` {
		t.Fatalf("future_field %s", rec.Extra["future_field"])
	}
	out, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	var again map[string]any
	if err := json.Unmarshal(out, &again); err != nil {
		t.Fatal(err)
	}
	if again["confidence"] != "low" {
		t.Fatalf("round trip %s", out)
	}
}

func TestCursorAdaptersUnchanged(t *testing.T) {
	if cursor.Version != "2" || cursor.Confidence != "low" {
		t.Fatalf("cursor pin version=%s confidence=%s", cursor.Version, cursor.Confidence)
	}
	if cursorcli.Version != "1" || cursorcli.Confidence != "low" {
		t.Fatalf("cursor-cli pin version=%s confidence=%s", cursorcli.Version, cursorcli.Confidence)
	}
	grokRel := "sessions/%2Fwork%2Fdemo/" + sessionUUIDFixture + "/updates.jsonl"
	if _, ok := (cursor.Adapter{}).Match(grokRel); ok {
		t.Fatal("cursor matched a grok transcript")
	}
	if _, ok := (cursorcli.Adapter{}).Match(grokRel); ok {
		t.Fatal("cursor-cli matched a grok transcript")
	}
	if kind, ok := (cursor.Adapter{}).Match("User/globalStorage/state.vscdb"); !ok || kind != protocol.KindCursorStateJSON {
		t.Fatalf("cursor match %s %v", kind, ok)
	}
	if kind, ok := (cursorcli.Adapter{}).Match("chats/ab/sid/store.db"); !ok || kind != protocol.KindCursorCLIStoreJSON {
		t.Fatalf("cursor-cli match %s %v", kind, ok)
	}
	if _, ok := (grok.Adapter{}).Match("User/globalStorage/state.vscdb"); ok {
		t.Fatal("grok matched a cursor database")
	}
	if _, ok := (grok.Adapter{}).Match("chats/ab/sid/store.db"); ok {
		t.Fatal("grok matched a cursor-cli store")
	}
	if _, ok := (grok.Adapter{}).Match("User/globalStorage/state.vscdb-wal"); ok {
		t.Fatal("grok matched a cursor wal")
	}
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
