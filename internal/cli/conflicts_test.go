package cli

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/auth"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/protocol"
)

func TestConflictsMissingCatalog(t *testing.T) {
	data := t.TempDir()
	var out bytes.Buffer
	err := Run([]string{"conflicts", "--data", data}, Env{Stdout: &out, Stderr: ioDiscard()})
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "divergent_copy: 0\n" {
		t.Fatalf("out %q", out.String())
	}
	if _, statErr := os.Stat(filepath.Join(data, "catalog.db")); !os.IsNotExist(statErr) {
		t.Fatalf("missing catalog was created: %v", statErr)
	}
}

func TestConflictsLocalList(t *testing.T) {
	data := t.TempDir()
	cat, err := catalog.Open(filepath.Join(data, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 22, 16, 0, 0, 0, time.UTC)
	headSHA := strings.Repeat("ab", 32)
	divSHA := strings.Repeat("cd", 32)
	m := protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessTerva,
		NativeSessionID: "sid-1",
		Artifacts: []protocol.Artifact{{
			Kind:    protocol.KindTranscriptJSONL,
			RelPath: "sessions/x/sid-1.jsonl",
			Size:    4,
			SHA256:  headSHA,
		}},
	}
	if _, err := cat.Ingest(t.Context(), m, now, []catalog.Decision{{
		Relation: protocol.RelationHead, Record: true, Head: true,
	}}, nil); err != nil {
		t.Fatal(err)
	}
	fork := m
	fork.MachineID = "machine-b"
	fork.Artifacts[0].SHA256 = divSHA
	fork.Artifacts[0].Size = 9
	ack, err := cat.Ingest(t.Context(), fork, now.Add(time.Minute), []catalog.Decision{{
		Relation: protocol.RelationDivergentCopy, Record: true,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cat.Close()

	var out bytes.Buffer
	if err := Run([]string{"conflicts", "--data", data}, Env{Stdout: &out, Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"divergent_copy: 1\n",
		"session_uid: " + ack.SessionUID,
		"artifact_id: " + ack.ArtifactIDs[0],
		"sha256: " + divSHA,
		"head_sha256: " + headSHA,
		"machines: machine-b",
		"head_machines: machine-a",
		"native_session_id: sid-1",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q\n%s", want, text)
		}
	}
}

func TestConflictsRemote(t *testing.T) {
	s, err := api.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.Allow("sekret")
	now := time.Date(2026, 9, 22, 16, 0, 0, 0, time.UTC)
	headSHA := strings.Repeat("ab", 32)
	divSHA := strings.Repeat("cd", 32)
	m := protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessTerva,
		NativeSessionID: "sid-1",
		Artifacts: []protocol.Artifact{{
			Kind:    protocol.KindTranscriptJSONL,
			RelPath: "sessions/x/sid-1.jsonl",
			Size:    4,
			SHA256:  headSHA,
		}},
	}
	if _, err := s.Catalog.Ingest(t.Context(), m, now, []catalog.Decision{{
		Relation: protocol.RelationHead, Record: true, Head: true,
	}}, nil); err != nil {
		t.Fatal(err)
	}
	fork := m
	fork.MachineID = "machine-b"
	fork.Artifacts[0].SHA256 = divSHA
	fork.Artifacts[0].Size = 9
	ack, err := s.Catalog.Ingest(t.Context(), fork, now.Add(time.Minute), []catalog.Decision{{
		Relation: protocol.RelationDivergentCopy, Record: true,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	token := filepath.Join(t.TempDir(), "token")
	if err := auth.Write(token, "sekret"); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	env := Env{
		Stderr: ioDiscard(),
		Getenv: func(k string) string {
			if k == "HOME" {
				return home
			}
			return ""
		},
	}

	var out bytes.Buffer
	env.Stdout = &out
	if err := Run([]string{"conflicts", "--server", srv.URL, "--token-file", token}, env); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"session_uid: " + ack.SessionUID,
		"artifact_id: " + ack.ArtifactIDs[0],
		"sha256: " + divSHA,
		"machines: machine-b",
		"head_machines: machine-a",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q\n%s", want, text)
		}
	}

	err = Run([]string{"conflicts", "--server", srv.URL}, env)
	if err == nil || !strings.Contains(err.Error(), "unauthorized") {
		t.Fatalf("err %v", err)
	}
	err = Run([]string{"conflicts", "--data", t.TempDir(), "--server", srv.URL}, env)
	if err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("err %v", err)
	}
}
