package api

import (
	"net/http"
	"slices"
	"testing"

	"terva.sh/lampi/internal/protocol"
)

// Paths as the Claude adapter walks them: the session's directory of
// subagent transcripts sorts before the session's own transcript.
const (
	mainRel = "projects/p/sid.jsonl"
	subRel  = "projects/p/sid/subagents/agent-a.jsonl"
)

func claudeLine(text string) []byte {
	return []byte(`{"type":"user","sessionId":"sid","timestamp":"2026-09-25T00:00:00Z","message":{"role":"user","content":"` + text + `"}}` + "\n")
}

func claudeArtifact(t *testing.T, h http.Handler, rel string, body []byte) protocol.Artifact {
	t.Helper()
	d := putRaw(t, h, body)
	return protocol.Artifact{Kind: protocol.KindTranscriptJSONL, RelPath: rel, Size: int64(len(body)), SHA256: d, TailSHA256: d}
}

func claudeManifest(arts ...protocol.Artifact) protocol.Manifest {
	m := manifest("m", "sid", nil, "", 0, "")
	m.Harness = protocol.HarnessClaude
	m.Artifacts = arts
	return m
}

// projectedText is the text of every event the session's normalization
// reads, sorted.
func projectedText(t *testing.T, s *Server, m protocol.Manifest) []string {
	t.Helper()
	evs, err := s.Project(t.Context(), m)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range evs {
		if e.ContentText != nil {
			out = append(out, *e.ContentText)
		}
	}
	slices.Sort(out)
	return out
}

func headRel(t *testing.T, s *Server) (string, []string) {
	t.Helper()
	v, _, err := s.Catalog.Head(t.Context(), protocol.HarnessClaude, "sid")
	if err != nil {
		t.Fatal(err)
	}
	var head string
	var current []string
	for _, a := range v.Current {
		current = append(current, a.RelPath)
		if a.SHA256 == v.HeadSHA256 {
			head = a.RelPath
		}
	}
	return head, current
}

// A manifest that lists a subagent transcript before the session's own
// makes the session's transcript the head, and normalization reads
// both (TKT-01M3M5VEQ). Before, the subagent became the head and the
// session's transcript was left out.
func TestSubagentListedFirstIsNotTheHead(t *testing.T) {
	s := openServer(t)
	h := s.Handler()
	m := claudeManifest(
		claudeArtifact(t, h, subRel, claudeLine("subagent text")),
		claudeArtifact(t, h, mainRel, claudeLine("main text")),
	)
	postManifest(t, h, m)
	head, current := headRel(t, s)
	if head != mainRel || len(current) != 2 {
		t.Fatalf("head %q, current %v", head, current)
	}
	if got := projectedText(t, s, m); !slices.Equal(got, []string{"main text", "subagent text"}) {
		t.Fatalf("projected %q", got)
	}
}

// A later sync that carries only a new subagent file, because the
// session's transcript did not change, adds that file beside the
// transcript. Before, the lake took it for the transcript moving and
// kept it as a divergent copy that nothing read.
func TestSubagentAloneInAManifestIsKept(t *testing.T) {
	s := openServer(t)
	h := s.Handler()
	postManifest(t, h, claudeManifest(claudeArtifact(t, h, mainRel, claudeLine("main text"))))
	m := claudeManifest(claudeArtifact(t, h, subRel, claudeLine("subagent text")))
	postManifest(t, h, m)
	head, current := headRel(t, s)
	if head != mainRel || len(current) != 2 {
		t.Fatalf("head %q, current %v", head, current)
	}
	if got := projectedText(t, s, m); !slices.Equal(got, []string{"main text", "subagent text"}) {
		t.Fatalf("projected %q", got)
	}
}
