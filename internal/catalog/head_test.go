package catalog

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"terva.sh/lampi/internal/protocol"
)

func currentOfKind(t *testing.T, c *Catalog, harness, native, kind string) []ArtifactRow {
	t.Helper()
	_, arts, ok, err := c.Current(context.Background(), harness, native)
	if err != nil || !ok {
		t.Fatalf("current ok=%v err=%v", ok, err)
	}
	var out []ArtifactRow
	for _, a := range arts {
		if a.Kind == kind {
			out = append(out, a)
		}
	}
	return out
}

func TestNewRelpathRelatesToSessionHead(t *testing.T) {
	c, err := Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	ctx := context.Background()
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	base := []byte("{\"n\":1}\n")
	grown := append(append([]byte{}, base...), []byte("{\"n\":2}\n")...)
	fork := append(append([]byte{}, base...), []byte("{\"n\":3}\n")...)
	errs := []byte("{\"error\":1}\n")
	blobs := memBlobs{digestHex(base): base, digestHex(grown): grown, digestHex(fork): fork, digestHex(errs): errs}
	relA := "sessions/aaaa/sess-1.jsonl"
	relB := "sessions/bbbb/sess-1.jsonl"
	post := func(machine, rel string, body []byte, extra ...protocol.Artifact) protocol.ManifestAck {
		t.Helper()
		m := protocol.Manifest{
			CaptureProtocol: protocol.Version,
			MachineID:       machine,
			Harness:         protocol.HarnessTerva,
			NativeSessionID: "sess-1",
			Artifacts: append([]protocol.Artifact{{
				Kind: protocol.KindTranscriptJSONL, RelPath: rel, Size: int64(len(body)), SHA256: digestHex(body),
			}}, extra...),
		}
		ds := make([]Decision, len(m.Artifacts))
		for i := range ds {
			ds[i] = Decision{Relation: protocol.RelationHead, Record: true, Head: i == 0}
		}
		now = now.Add(time.Minute)
		ack, err := c.Ingest(ctx, m, now, ds, blobs)
		if err != nil {
			t.Fatal(err)
		}
		return ack
	}
	oneHead := func(wantRel, wantSHA string) {
		t.Helper()
		heads := currentOfKind(t, c, protocol.HarnessTerva, "sess-1", protocol.KindTranscriptJSONL)
		if len(heads) != 1 || heads[0].RelPath != wantRel || heads[0].SHA256 != wantSHA {
			t.Fatalf("current transcripts %+v, want one at %s", heads, wantRel)
		}
	}

	first := post("machine-a", relA, base)
	oneHead(relA, digestHex(base))

	// The same bytes under a second machine's path are the head.
	same := post("machine-b", relB, base)
	if same.Relation != protocol.RelationUnchanged || same.ArtifactIDs[0] != first.ArtifactIDs[0] {
		t.Fatalf("same bytes: %+v", same)
	}
	oneHead(relA, digestHex(base))

	// An extension under that path moves the head there. The error
	// sidecar keeps its own path.
	moved := post("machine-b", relB, grown, protocol.Artifact{
		Kind: protocol.KindErrorsJSONL, RelPath: "sessions/bbbb/sess-1.errors.jsonl", Size: int64(len(errs)), SHA256: digestHex(errs),
	})
	if moved.Relation != protocol.RelationGrownFrom || moved.HeadSHA256 != digestHex(grown) {
		t.Fatalf("grown: %+v", moved)
	}
	oneHead(relB, digestHex(grown))
	if side := currentOfKind(t, c, protocol.HarnessTerva, "sess-1", protocol.KindErrorsJSONL); len(side) != 1 {
		t.Fatalf("error sidecar %+v", side)
	}

	// The first machine's shorter file is stale against that head.
	stale := post("machine-a", relA, base)
	if stale.Relation != protocol.RelationStale || stale.HeadSHA256 != digestHex(grown) || stale.HeadSize != int64(len(grown)) || stale.ArtifactIDs[0] != moved.ArtifactIDs[0] {
		t.Fatalf("stale: %+v", stale)
	}
	oneHead(relB, digestHex(grown))

	// Bytes that are not a prefix are a copy, not a second head.
	div := post("machine-a", relA, fork)
	if div.Relation != protocol.RelationDivergentCopy || div.HeadSHA256 != digestHex(grown) {
		t.Fatalf("divergent: %+v", div)
	}
	oneHead(relB, digestHex(grown))
	copies, err := c.DivergentCopies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(copies) != 1 || copies[0].RelPath != relA || len(copies[0].HeadMachines) != 1 || copies[0].HeadMachines[0] != "machine-b" {
		t.Fatalf("copies %+v", copies)
	}
}

func TestSnapshotHeadMovesAcrossRelpaths(t *testing.T) {
	c, err := Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	ctx := context.Background()
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	one := []byte(`{"info":{"id":"ses_1"},"messages":[{"n":1}]}`)
	two := []byte(`{"info":{"id":"ses_1"},"messages":[{"n":1},{"n":2}]}`)
	three := []byte(`{"info":{"id":"ses_1"},"messages":[{"n":1},{"n":2},{"n":3}]}`)
	blobs := memBlobs{digestHex(one): one, digestHex(two): two, digestHex(three): three}
	post := func(machine, rel string, body []byte) protocol.ManifestAck {
		t.Helper()
		m := protocol.Manifest{
			CaptureProtocol: protocol.Version,
			MachineID:       machine,
			Harness:         protocol.HarnessOpenCode,
			NativeSessionID: "ses_1",
			Artifacts: []protocol.Artifact{{
				Kind: protocol.KindOpenCodeExportJSON, RelPath: rel, Size: int64(len(body)), SHA256: digestHex(body),
			}},
		}
		now = now.Add(time.Minute)
		ack, err := c.Ingest(ctx, m, now, []Decision{{Relation: protocol.RelationHead, Record: true, Head: true}}, blobs)
		if err != nil {
			t.Fatal(err)
		}
		return ack
	}
	oneHead := func(wantRel, wantSHA string) {
		t.Helper()
		heads := currentOfKind(t, c, protocol.HarnessOpenCode, "ses_1", protocol.KindOpenCodeExportJSON)
		if len(heads) != 1 || heads[0].RelPath != wantRel || heads[0].SHA256 != wantSHA {
			t.Fatalf("current exports %+v, want one at %s", heads, wantRel)
		}
	}

	post("machine-a", "export/ses_1.json", one)
	// A re-export is a new JSON object, not an extension. It moves the head.
	again := post("machine-a", "export/ses_1.json", two)
	if again.Relation != protocol.RelationHead || again.HeadSHA256 != digestHex(two) {
		t.Fatalf("re-export: %+v", again)
	}
	oneHead("export/ses_1.json", digestHex(two))

	moved := post("machine-b", "export/2026-09-25/ses_1.json", three)
	if moved.Relation != protocol.RelationHead || moved.HeadSHA256 != digestHex(three) {
		t.Fatalf("second path: %+v", moved)
	}
	oneHead("export/2026-09-25/ses_1.json", digestHex(three))
	copies, err := c.DivergentCopies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(copies) != 0 {
		t.Fatalf("re-export stored a divergent copy: %+v", copies)
	}
}
