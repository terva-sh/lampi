package protocol

import (
	"encoding/json"
	"testing"
	"time"
)

func TestManifestRoundTrip(t *testing.T) {
	raw := []byte(`{
	  "capture_protocol": 1,
	  "machine_id": "01ARZ3NDEKTSV4RRFFQ69G5FAV",
	  "harness": "terva",
	  "harness_version": "0.137.0",
	  "native_session_id": "20260922-161000-abcd1234",
	  "project": {
	    "cwd": "/home/drew/src/foo",
	    "cwd_hash": "a1b2c3d4e5f60708",
	    "git_remote": "git@github.com:org/foo.git",
	    "git_commit": "abc"
	  },
	  "artifacts": [
	    {
	      "kind": "transcript_jsonl",
	      "relpath": "sessions/a1b2c3d4e5f60708/20260922-161000-abcd1234.jsonl",
	      "size": 120400,
	      "mtime": "2026-09-22T16:10:00Z",
	      "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	      "chunk_sha256s": null,
	      "byte_watermark_prev": 100000,
	      "tail_sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	      "redaction": {"status": "scanned", "ruleset": "v1", "hits": 0}
	    }
	  ],
	  "lineage": {"parent_native_id": null, "fork_point": null}
	}`)

	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.CaptureProtocol != Version || m.Harness != HarnessTerva {
		t.Fatalf("header: %+v", m)
	}
	if m.Project.CWD != "/home/drew/src/foo" || m.Project.CWDHash != "a1b2c3d4e5f60708" {
		t.Fatalf("project: %+v", m.Project)
	}
	if len(m.Artifacts) != 1 {
		t.Fatalf("artifacts: %d", len(m.Artifacts))
	}
	a := m.Artifacts[0]
	if a.Kind != KindTranscriptJSONL || a.Size != 120400 || a.ByteWatermarkPrev != 100000 {
		t.Fatalf("artifact: %+v", a)
	}
	if a.ChunkSHA256s != nil {
		t.Fatalf("chunk list should stay null, got %#v", a.ChunkSHA256s)
	}
	if !a.MTime.Equal(time.Date(2026, 9, 22, 16, 10, 0, 0, time.UTC)) {
		t.Fatalf("mtime: %s", a.MTime)
	}
	// JSON null lands in RawMessage as the bytes "null", not a nil slice.
	if m.Lineage.ParentNativeID != nil || string(m.Lineage.ForkPoint) != "null" {
		t.Fatalf("lineage: %+v", m.Lineage)
	}
	if a.Redaction.Hits != 0 || a.Redaction.Ruleset != "v1" {
		t.Fatalf("redaction: %+v", a.Redaction)
	}

	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var again Manifest
	if err := json.Unmarshal(out, &again); err != nil {
		t.Fatal(err)
	}
	if again.NativeSessionID != m.NativeSessionID || again.Artifacts[0].SHA256 != a.SHA256 {
		t.Fatalf("round trip: %+v", again)
	}
}

func TestValidDigest(t *testing.T) {
	ok := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if !ValidDigest(ok) {
		t.Fatal("expected valid")
	}
	for _, bad := range []string{"", ok[:63], ok + "a", "ABCDEF" + ok[6:], ok[:62] + "zz"} {
		if ValidDigest(bad) {
			t.Fatalf("accepted %q", bad)
		}
	}
}
