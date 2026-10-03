package grokbot_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/adapter/grokbot"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/testharness"
)

const sessionUUIDFixture = "018f1a2b-3c4d-7e5f-8a9b-0c1d2e3f4a5b"

func TestHome(t *testing.T) {
	got, err := grokbot.Home(func(k string) string {
		if k == "GROK_BOT_HOME" {
			return "/tmp/grokbot-alt"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/grokbot-alt" {
		t.Fatalf("override %s", got)
	}

	got, err = grokbot.Home(func(k string) string {
		switch k {
		case "HOME":
			return "/home/box"
		case "GROK_HOME":
			return "/home/box/.grok"
		case "XDG_CONFIG_HOME":
			return "/xdg"
		default:
			return ""
		}
	})
	if err == nil {
		t.Fatalf("home %s followed a grok or home default", got)
	}
	if _, err := grokbot.Home(func(string) string { return "" }); err == nil {
		t.Fatal("missing GROK_BOT_HOME should fail")
	}
}

func TestDiscoverPlant(t *testing.T) {
	root := t.TempDir()
	res, err := testharness.PlantGrokBot(root, []testharness.SessionSpec{{
		ID: sessionUUIDFixture, Prompt: "hello grokbot",
	}})
	if err != nil {
		t.Fatal(err)
	}
	refs, err := grokbot.Adapter{}.Discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 {
		t.Fatalf("discovered %d: %+v", len(refs), refs)
	}
	want := "agent-transcripts/" + sessionUUIDFixture + "/" + sessionUUIDFixture + ".jsonl"
	if refs[0].RelPath != want || refs[0].Kind != protocol.KindTranscriptJSONL {
		t.Fatalf("ref %+v", refs[0])
	}
	if refs[0].AbsPath != res.Files[0] {
		t.Fatalf("abs %s want %s", refs[0].AbsPath, res.Files[0])
	}
}

func TestDiscoverAgentDataNoise(t *testing.T) {
	root := t.TempDir()
	if _, err := testharness.PlantGrokBot(root, []testharness.SessionSpec{{
		ID: sessionUUIDFixture, Prompt: "hello grokbot",
	}}); err != nil {
		t.Fatal(err)
	}
	other := "11111111-2222-4333-8444-555555555555"
	sessionDir := filepath.Join(root, "agent-transcripts", sessionUUIDFixture)
	// A root shaped like agent-data: databases, a journal, and paths
	// that are not agent-transcripts/<uuid>/<same-uuid>.jsonl.
	mustWrite(t, filepath.Join(root, "store.db"), "sqlite-not-a-transcript")
	mustWrite(t, filepath.Join(root, "conversation-blobs.db"), "key-material-not-read")
	mustWrite(t, filepath.Join(sessionDir, "store.db"), "sqlite-beside-transcript")
	mustWrite(t, filepath.Join(sessionDir, "conversation-blobs.db"), "blobs-beside-transcript")
	mustWrite(t, filepath.Join(sessionDir, sessionUUIDFixture+".jsonl.journal-mode"), "{}\n")
	mustWrite(t, filepath.Join(sessionDir, "notes.txt"), "no")
	mustWrite(t, filepath.Join(root, "agent-transcripts", other, sessionUUIDFixture+".jsonl"), "{}\n")
	mustWrite(t, filepath.Join(root, "agent-transcripts", other, other+".json"), "{}\n")
	mustWrite(t, filepath.Join(root, "agent-transcripts", "not-a-uuid", "not-a-uuid.jsonl"), "{}\n")
	mustWrite(t, filepath.Join(root, "sessions", "updates.jsonl"), "{}\n")

	refs, err := grokbot.Adapter{}.Discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 {
		t.Fatalf("discovered %d: %+v", len(refs), refs)
	}
	want := "agent-transcripts/" + sessionUUIDFixture + "/" + sessionUUIDFixture + ".jsonl"
	if refs[0].RelPath != want || refs[0].Kind != protocol.KindTranscriptJSONL {
		t.Fatalf("ref %+v", refs[0])
	}
	for _, r := range refs {
		if strings.Contains(r.RelPath, "store.db") || strings.Contains(r.RelPath, "conversation-blobs") || strings.HasSuffix(r.RelPath, ".journal-mode") {
			t.Fatalf("discovered a file this pin does not read: %s", r.RelPath)
		}
	}
}

func TestManifestCWDIsAgentDir(t *testing.T) {
	if grokbot.Version != "1" {
		t.Fatalf("adapter version %q", grokbot.Version)
	}
	root := t.TempDir()
	res, err := testharness.PlantGrokBot(root, []testharness.SessionSpec{{
		ID: sessionUUIDFixture, Prompt: "hello grokbot",
	}})
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(filepath.Dir(res.Files[0]), "store.db"), "not-uploaded")
	mustWrite(t, filepath.Join(filepath.Dir(res.Files[0]), "conversation-blobs.db"), "not-uploaded")

	b, err := grokbot.Manifests(root, "machine-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Manifests) != 1 {
		t.Fatalf("manifests %d skipped %v", len(b.Manifests), b.Skipped)
	}
	m := b.Manifests[0]
	if m.Harness != protocol.HarnessGrokBot || m.HarnessVersion != grokbot.Version || m.CaptureProtocol != 1 {
		t.Fatalf("header %+v", m)
	}
	if m.NativeSessionID != sessionUUIDFixture {
		t.Fatalf("native id %s", m.NativeSessionID)
	}
	agentDir := filepath.Join(root, "agent-transcripts", sessionUUIDFixture)
	if m.Project.CWD != agentDir {
		t.Fatalf("cwd %q want %q", m.Project.CWD, agentDir)
	}
	info, err := os.Stat(m.Project.CWD)
	if err != nil || !info.IsDir() {
		t.Fatalf("agent dir %s: %v", m.Project.CWD, err)
	}
	if m.Project.CWDHash == "" {
		t.Fatal("cwd hash empty")
	}
	if len(m.Artifacts) != 1 || m.Artifacts[0].Kind != protocol.KindTranscriptJSONL {
		t.Fatalf("artifacts %+v", m.Artifacts)
	}
	if m.Artifacts[0].RelPath != "agent-transcripts/"+sessionUUIDFixture+"/"+sessionUUIDFixture+".jsonl" {
		t.Fatalf("rel %s", m.Artifacts[0].RelPath)
	}
	if _, ok := b.Paths["agent-transcripts/"+sessionUUIDFixture+"/store.db"]; ok {
		t.Fatal("store.db was uploaded")
	}
	// An allowlist is a path prefix. An empty cwd is refused when Allow
	// is non-empty, so the manifest cwd has to be this agent directory.
	allow := config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: root}}}
	if reason := allow.Refusal(config.ProjectID{CWD: m.Project.CWD}); reason != "" {
		t.Fatalf("agent dir refused: %s", reason)
	}
	if reason := allow.Refusal(config.ProjectID{}); reason != config.RefusedNoCWD {
		t.Fatalf("empty cwd refusal %q", reason)
	}
}

func TestMatchSkipsJournalAndDatabases(t *testing.T) {
	id := sessionUUIDFixture
	other := "11111111-2222-4333-8444-555555555555"
	match := "agent-transcripts/" + id + "/" + id + ".jsonl"
	kind, ok := (grokbot.Adapter{}).Match(match)
	if !ok || kind != protocol.KindTranscriptJSONL {
		t.Fatalf("match %s %v", kind, ok)
	}
	for _, rel := range []string{
		"agent-transcripts/" + id + "/" + id + ".jsonl.journal-mode",
		"agent-transcripts/" + id + "/store.db",
		"agent-transcripts/" + id + "/conversation-blobs.db",
		"agent-transcripts/" + id + "/" + other + ".jsonl",
		"agent-transcripts/" + other + "/" + id + ".jsonl",
		"store.db",
		"conversation-blobs.db",
		"sessions/" + id + "/updates.jsonl",
	} {
		if _, ok := (grokbot.Adapter{}).Match(rel); ok {
			t.Fatalf("matched %s", rel)
		}
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
