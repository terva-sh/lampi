package catalog

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/protocol"
)

func TestIngestStableIDs(t *testing.T) {
	c, err := Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })

	m := sampleManifest()
	now := time.Date(2026, 9, 22, 16, 0, 0, 0, time.UTC)
	ctx := context.Background()
	first, err := c.Ingest(ctx, m, now)
	if err != nil {
		t.Fatal(err)
	}
	if first.SessionUID == "" || len(first.ArtifactIDs) != 1 || first.HeadSHA256 != m.Artifacts[0].SHA256 {
		t.Fatalf("ack: %+v", first)
	}
	second, err := c.Ingest(ctx, m, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if second.SessionUID != first.SessionUID || second.ArtifactIDs[0] != first.ArtifactIDs[0] {
		t.Fatalf("repeat changed ids: %+v then %+v", first, second)
	}

	grown := sampleManifest()
	grown.Artifacts[0].SHA256 = strings.Repeat("ab", 32)
	grown.Artifacts[0].Size = 4
	third, err := c.Ingest(ctx, grown, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if third.SessionUID != first.SessionUID {
		t.Fatal("grown session minted a new uid")
	}
	if third.ArtifactIDs[0] == first.ArtifactIDs[0] {
		t.Fatal("new digest reused the old artifact id")
	}
	if third.HeadSHA256 != grown.Artifacts[0].SHA256 {
		t.Fatalf("head: %s", third.HeadSHA256)
	}

	other := sampleManifest()
	other.MachineID = "machine-b"
	fourth, err := c.Ingest(ctx, other, now)
	if err != nil {
		t.Fatal(err)
	}
	if fourth.SessionUID != first.SessionUID {
		t.Fatal("second machine did not join the logical session")
	}
}

func sampleManifest() protocol.Manifest {
	sha := strings.Repeat("a", 64)
	return protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessTerva,
		NativeSessionID: "20260922-161000-abcd1234",
		Artifacts: []protocol.Artifact{{
			Kind:    protocol.KindTranscriptJSONL,
			RelPath: "sessions/x/20260922-161000-abcd1234.jsonl",
			Size:    3,
			SHA256:  sha,
		}},
	}
}
