package api

import (
	"bytes"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/adapter/opencode"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/protocol"
)

func TestOpenCodeReexportMovesHead(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.Allow("sekret")
	h := s.Handler()

	msg := func(id, role, text string, at int64) string {
		created := strconv.FormatInt(1758801600000+at*1000, 10)
		return `{"info":{"id":"` + id + `","role":"` + role + `","time":{"created":` + created + `}},"parts":[{"type":"text","text":"` + text + `"}]}`
	}
	doc := func(msgs ...string) []byte {
		return []byte(`{"info":{"id":"ses_1","directory":"/work/app"},"messages":[` + strings.Join(msgs, ",") + `]}` + "\n")
	}
	first := doc(msg("msg_1", "user", "first pond", 1))
	later := doc(msg("msg_1", "user", "first pond", 1), msg("msg_2", "assistant", "later pond", 2))
	post := func(machine, rel string, body []byte) protocol.ManifestAck {
		t.Helper()
		sum := putBlob(t, h, "", body)
		return postManifest(t, h, protocol.Manifest{
			CaptureProtocol: protocol.Version,
			MachineID:       machine,
			Harness:         protocol.HarnessOpenCode,
			HarnessVersion:  opencode.Version,
			NativeSessionID: "ses_1",
			Project:         protocol.Project{CWD: "/work/app", CWDHash: adapter.CWDHash("/work/app")},
			Artifacts: []protocol.Artifact{{
				Kind:    protocol.KindOpenCodeExportJSON,
				RelPath: rel,
				Size:    int64(len(body)),
				SHA256:  sum,
			}},
		})
	}

	ack := post("machine-a", "export/ses_1.json", first)
	derived := readDerived(t, s, ack.SessionUID)
	if !bytes.Contains(derived, []byte("first pond")) || bytes.Contains(derived, []byte("later pond")) {
		t.Fatalf("first export:\n%s", derived)
	}

	again := post("machine-a", "export/ses_1.json", later)
	if again.SessionUID != ack.SessionUID || again.Relation != protocol.RelationHead || again.HeadSHA256 != shaOf(t, later) {
		t.Fatalf("re-export ack %+v", again)
	}
	derived = readDerived(t, s, ack.SessionUID)
	if !bytes.Contains(derived, []byte("later pond")) || bytes.Count(derived, []byte("first pond")) != 1 {
		t.Fatalf("re-export:\n%s", derived)
	}
	copies, err := s.Catalog.DivergentCopies(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(copies) != 0 {
		t.Fatalf("re-export stored a divergent copy: %+v", copies)
	}
}

func TestSessionUnderSecondRelpathProjectsOnce(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.Allow("sekret")
	h := s.Handler()

	line := func(role, text, at string) string {
		return `{"type":"` + role + `","sessionId":"sid-x","cwd":"/work/app","timestamp":"2026-09-25T12:00:0` + at + `.000Z","message":{"role":"` + role + `","content":"` + text + `"}}`
	}
	main := transcriptLines(line("user", "main pond", "1"))
	grown := transcriptLines(line("user", "main pond", "1"), line("user", "later pond", "2"))
	sub := transcriptLines(line("user", "subagent pond", "3"))
	post := func(machine, home string, body []byte) protocol.ManifestAck {
		t.Helper()
		mainSHA := putBlob(t, h, "", body)
		subSHA := putBlob(t, h, "", sub)
		return postManifest(t, h, protocol.Manifest{
			CaptureProtocol: protocol.Version,
			MachineID:       machine,
			Harness:         protocol.HarnessClaude,
			HarnessVersion:  "1",
			NativeSessionID: "sid-x",
			Project:         protocol.Project{CWD: "/work/app", CWDHash: adapter.CWDHash("/work/app")},
			Artifacts: []protocol.Artifact{
				{Kind: protocol.KindTranscriptJSONL, RelPath: "projects/" + home + "/sid-x.jsonl", Size: int64(len(body)), SHA256: mainSHA},
				{Kind: protocol.KindTranscriptJSONL, RelPath: "projects/" + home + "/sid-x/subagents/agent-a.jsonl", Size: int64(len(sub)), SHA256: subSHA},
			},
		})
	}

	first := post("machine-a", "-home-a-app", main)
	derived := readDerived(t, s, first.SessionUID)
	if bytes.Count(derived, []byte("main pond")) != 1 || bytes.Count(derived, []byte("subagent pond")) != 1 {
		t.Fatalf("first post:\n%s", derived)
	}

	// The same session from a second machine, whose relpaths embed a
	// different home, extends the transcript.
	second := post("machine-b", "-home-b-app", grown)
	if second.SessionUID != first.SessionUID || second.Relation != protocol.RelationGrownFrom || second.HeadSHA256 != shaOf(t, grown) {
		t.Fatalf("second ack %+v", second)
	}
	_, arts, ok, err := s.Catalog.Current(t.Context(), protocol.HarnessClaude, "sid-x")
	if err != nil || !ok {
		t.Fatalf("current ok=%v err=%v", ok, err)
	}
	var mains []string
	for _, a := range arts {
		if !strings.Contains(a.RelPath, "/subagents/") {
			mains = append(mains, a.RelPath)
		}
	}
	if len(mains) != 1 || mains[0] != "projects/-home-b-app/sid-x.jsonl" {
		t.Fatalf("current transcripts %v", mains)
	}
	derived = readDerived(t, s, first.SessionUID)
	if bytes.Count(derived, []byte("main pond")) != 1 || bytes.Count(derived, []byte("later pond")) != 1 || bytes.Count(derived, []byte("subagent pond")) != 1 {
		t.Fatalf("second post:\n%s", derived)
	}

	// The first machine posting its older file does not move the head
	// back or add a second copy to the projection.
	stale := post("machine-a", "-home-a-app", main)
	if stale.Relation != protocol.RelationStale || stale.HeadSHA256 != shaOf(t, grown) {
		t.Fatalf("stale ack %+v", stale)
	}
	derived = readDerived(t, s, first.SessionUID)
	if bytes.Count(derived, []byte("main pond")) != 1 || bytes.Count(derived, []byte("subagent pond")) != 1 {
		t.Fatalf("after stale:\n%s", derived)
	}
}

// The OpenCode database label is local to the adapter. The allowlist
// keeps it on the machine, and the lake refuses it as an unknown kind.
func TestOpenCodeDatabaseKindIsRefused(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.Allow("sekret")
	h := s.Handler()

	body := []byte("SQLite format 3\x00")
	sum := putBlob(t, h, "", body)
	rr := postManifestCode(t, h, protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessOpenCode,
		HarnessVersion:  opencode.Version,
		NativeSessionID: "opencode.db",
		Artifacts: []protocol.Artifact{{
			Kind:    opencode.KindDatabase,
			RelPath: "opencode.db",
			Size:    int64(len(body)),
			SHA256:  sum,
		}},
	})
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), opencode.KindDatabase) {
		t.Fatalf("database kind: %d %s", rr.Code, rr.Body)
	}
	n, err := s.Catalog.Counts(t.Context())
	if err != nil || n.Sessions != 0 {
		t.Fatalf("counts %+v err=%v", n, err)
	}
}

// A head with no directory takes its top-level peers only. A current
// row under another directory is not the same session's sidecar.
func TestFlatHeadSkipsNestedRows(t *testing.T) {
	v := catalog.HeadView{
		HeadSHA256: "h",
		Current: []catalog.ArtifactRow{
			{RelPath: "sid.jsonl", SHA256: "h"},
			{RelPath: "sid.errors.jsonl", SHA256: "e"},
			{RelPath: "old/sid.jsonl", SHA256: "o"},
		},
	}
	got := headArtifacts(protocol.HarnessTerva, v)
	if len(got) != 2 || got[0].SHA256 != "h" || got[1].SHA256 != "e" {
		t.Fatalf("flat head: %+v", got)
	}
}
