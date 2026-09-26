package api

import (
	"bytes"
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"

	"terva.sh/lampi/internal/cas"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/protocol"
)

func TestManifestAckDoesNotWaitForNormalize(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	var once sync.Once
	letGo := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() { s.Close() })
	t.Cleanup(letGo)
	started := make(chan struct{})
	s.beforeProject = func() {
		close(started)
		<-release
	}
	s.Allow("sekret")
	h := s.Handler()

	body := transcriptLines(
		`{"type":"meta","meta":{"id":"sid-async","cwd":"/tmp","started":"2026-09-22T16:10:00Z","version":"0.1.0"}}`,
		`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"async pond"}],"time":"2026-09-22T16:10:01Z"}}`,
	)
	sum := putBlob(t, h, "", body)
	ackCh := make(chan protocol.ManifestAck, 1)
	go func() {
		ackCh <- postManifest(t, h, protocol.Manifest{
			CaptureProtocol: protocol.Version,
			MachineID:       "machine-a",
			Harness:         protocol.HarnessTerva,
			NativeSessionID: "sid-async",
			Artifacts: []protocol.Artifact{{
				Kind:    protocol.KindTranscriptJSONL,
				RelPath: "sessions/x/sid-async.jsonl",
				Size:    int64(len(body)),
				SHA256:  sum,
			}},
		})
	}()

	var ack protocol.ManifestAck
	select {
	case ack = <-ackCh:
	case <-time.After(2 * time.Second):
		t.Fatal("manifest ACK waited on normalize")
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("normalize worker did not start")
	}
	if _, err := os.Stat(s.Normalized + "/" + ack.SessionUID + ".jsonl"); !os.IsNotExist(err) {
		t.Fatalf("jsonl existed while normalize was blocked: %v", err)
	}
	parts, err := normalize.SessionParquet(s.Parquet, ack.SessionUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 0 {
		t.Fatalf("parquet existed while normalize was blocked: %v", parts)
	}

	letGo()
	if state, err := s.Catalog.NormalizationState(t.Context(), ack.SessionUID); err != nil || (state != "pending" && state != "ready") {
		t.Fatalf("publication state %q: %v", state, err)
	}
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	if state, err := s.Catalog.NormalizationState(t.Context(), ack.SessionUID); err != nil || state != "ready" {
		t.Fatalf("published state %q: %v", state, err)
	}
	path, err := normalize.ParquetPath(s.Parquet, "2026-09-22", protocol.HarnessTerva, ack.SessionUID)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := parquet.ReadFile[normalize.ParquetRow](path)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, row := range rows {
		if row.ContentText != nil && *row.ContentText == "async pond" && row.Harness == protocol.HarnessTerva {
			found = true
		}
	}
	if !found {
		t.Fatalf("parquet rows %+v", rows)
	}
}

func TestParquetPartitionedByDateAndHarness(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.Allow("sekret")
	h := s.Handler()
	body := transcriptLines(
		`{"type":"meta","meta":{"id":"sid-days","cwd":"/tmp","started":"2026-09-22T16:10:00Z","version":"0.1.0"}}`,
		`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"monday pond"}],"time":"2026-09-22T16:10:01Z"}}`,
		`{"type":"message","message":{"role":"assistant","content":[{"type":"text","text":"tuesday pond"}],"time":"2026-09-23T01:00:00Z"}}`,
	)
	sum := putBlob(t, h, "", body)
	ack := postManifest(t, h, protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessTerva,
		NativeSessionID: "sid-days",
		Artifacts: []protocol.Artifact{{
			Kind:    protocol.KindTranscriptJSONL,
			RelPath: "sessions/x/sid-days.jsonl",
			Size:    int64(len(body)),
			SHA256:  sum,
		}},
	})
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, date := range []string{"2026-09-22", "2026-09-23"} {
		path, err := normalize.ParquetPath(s.Parquet, date, protocol.HarnessTerva, ack.SessionUID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing %s: %v", path, err)
		}
	}
	day2, err := normalize.ParquetPath(s.Parquet, "2026-09-23", protocol.HarnessTerva, ack.SessionUID)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := parquet.ReadFile[normalize.ParquetRow](day2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ContentText == nil || *rows[0].ContentText != "tuesday pond" {
		t.Fatalf("tuesday partition %+v", rows)
	}
	if rows[0].Harness != protocol.HarnessTerva {
		t.Fatalf("harness %s", rows[0].Harness)
	}
}

func TestNormalizeFailureRemovesParquet(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.Allow("sekret")
	h := s.Handler()
	good := transcriptLines(
		`{"type":"meta","meta":{"id":"sid-drop","cwd":"/tmp","started":"2026-09-22T16:10:00Z","version":"0.1.0"}}`,
		`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"kept pond"}],"time":"2026-09-22T16:10:01Z"}}`,
	)
	goodSHA := putBlob(t, h, "", good)
	ack := postManifest(t, h, manifest("machine-a", "sid-drop", good, goodSHA, 0, goodSHA))
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	path, err := normalize.ParquetPath(s.Parquet, "2026-09-22", protocol.HarnessTerva, ack.SessionUID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}

	tail := []byte("not-json\n")
	full := append(append([]byte{}, good...), tail...)
	tailSHA := putBlob(t, h, "", tail)
	fullSHA := shaOf(t, full)
	grown := postManifest(t, h, manifest("machine-a", "sid-drop", full, fullSHA, int64(len(good)), tailSHA))
	if grown.SessionUID != ack.SessionUID || grown.Relation != protocol.RelationGrownFrom {
		t.Fatalf("grown ack %+v", grown)
	}
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	msg, ok, err := s.Catalog.NormalizeError(t.Context(), ack.SessionUID)
	if err != nil || !ok || msg == "" {
		t.Fatalf("normalize_error %q ok=%v err=%v", msg, ok, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("parquet after failure: %v", err)
	}
	if got := readBlobBytes(t, s, goodSHA); !bytes.Equal(got, good) {
		t.Fatal("raw prefix changed")
	}
}

func TestUnimplementedHarnessRecordsNormalizeError(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.Allow("sekret")
	h := s.Handler()
	body := []byte("{}\n")
	for _, harness := range []string{protocol.HarnessCursor, protocol.HarnessCursorCLI} {
		sum := putBlob(t, h, "", body)
		ack := postManifest(t, h, protocol.Manifest{
			CaptureProtocol: protocol.Version,
			MachineID:       "machine-a",
			Harness:         harness,
			NativeSessionID: "sid-" + harness,
			Artifacts: []protocol.Artifact{{
				Kind:    protocol.KindTranscriptJSONL,
				RelPath: "export/sid-" + harness + ".json",
				Size:    int64(len(body)),
				SHA256:  sum,
			}},
		})
		if err := s.WaitNormalized(t.Context()); err != nil {
			t.Fatal(err)
		}
		msg, ok, err := s.Catalog.NormalizeError(t.Context(), ack.SessionUID)
		if err != nil || !ok || msg == "" {
			t.Fatalf("%s normalize_error %q ok=%v err=%v", harness, msg, ok, err)
		}
		parts, err := normalize.SessionParquet(s.Parquet, ack.SessionUID)
		if err != nil {
			t.Fatal(err)
		}
		if len(parts) != 0 {
			t.Fatalf("parquet for an unimplemented harness: %v", parts)
		}
	}

	// A valid Cursor IDE export is projected. The transcript_jsonl case
	// above still records normalize_error.
	valid := []byte(`{"harness_version":"1","confidence":"low","source":"state.vscdb","scope":"workspace","item_table":[{"key":"composer.composerHeaders","value":{"allComposers":[]}}]}`)
	validSum := putBlob(t, h, "", valid)
	validAck := postManifest(t, h, protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessCursor,
		HarnessVersion:  "1",
		NativeSessionID: "workspace/ws-valid",
		Artifacts: []protocol.Artifact{{
			Kind:    protocol.KindCursorStateJSON,
			RelPath: "User/workspaceStorage/ws-valid/state.json",
			Size:    int64(len(valid)),
			SHA256:  validSum,
		}},
	})
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	msg, ok, err := s.Catalog.NormalizeError(t.Context(), validAck.SessionUID)
	if err != nil || !ok || msg != "" {
		t.Fatalf("valid cursor normalize_error %q ok=%v err=%v", msg, ok, err)
	}
	derived := readDerived(t, s, validAck.SessionUID)
	if !bytes.Contains(derived, []byte(`"session_id":"cursor:workspace/ws-valid"`)) || !bytes.Contains(derived, []byte(`"schema_version":1`)) || !bytes.Contains(derived, []byte(`"harness":"cursor"`)) {
		t.Fatalf("valid cursor derived:\n%s", derived)
	}
	parts, err := normalize.SessionParquet(s.Parquet, validAck.SessionUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 1 || !bytes.Contains([]byte(parts[0]), []byte("harness="+protocol.HarnessCursor)) {
		t.Fatalf("valid cursor parquet: %v", parts)
	}
	if got := readBlobBytes(t, s, validSum); !bytes.Equal(got, valid) {
		t.Fatal("valid cursor CAS object changed")
	}

	// A valid Cursor CLI export is projected. The transcript_jsonl case
	// above still records normalize_error.
	validCLI := []byte(`{"harness_version":"1","confidence":"low","source":"chats/ab12/sid-valid/store.db","scope":"session","meta":[{"key":"0","value":{"name":"cli"}}],"blobs":[{"id":"user","data":{"role":"user","content":"cli pond"}}]}`)
	validCLISum := putBlob(t, h, "", validCLI)
	validCLIAck := postManifest(t, h, protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessCursorCLI,
		HarnessVersion:  "1",
		NativeSessionID: "chats/ab12/sid-valid",
		Artifacts: []protocol.Artifact{{
			Kind:    protocol.KindCursorCLIStoreJSON,
			RelPath: "chats/ab12/sid-valid/store.json",
			Size:    int64(len(validCLI)),
			SHA256:  validCLISum,
		}},
	})
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	msg, ok, err = s.Catalog.NormalizeError(t.Context(), validCLIAck.SessionUID)
	if err != nil || !ok || msg != "" {
		t.Fatalf("valid cursor-cli normalize_error %q ok=%v err=%v", msg, ok, err)
	}
	derived = readDerived(t, s, validCLIAck.SessionUID)
	if !bytes.Contains(derived, []byte(`"session_id":"cursor-cli:chats/ab12/sid-valid"`)) || !bytes.Contains(derived, []byte(`"schema_version":1`)) || !bytes.Contains(derived, []byte(`"harness":"cursor-cli"`)) {
		t.Fatalf("valid cursor-cli derived:\n%s", derived)
	}
	parts, err = normalize.SessionParquet(s.Parquet, validCLIAck.SessionUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 1 || !bytes.Contains([]byte(parts[0]), []byte("harness="+protocol.HarnessCursorCLI)) {
		t.Fatalf("valid cursor-cli parquet: %v", parts)
	}
	if got := readBlobBytes(t, s, validCLISum); !bytes.Equal(got, validCLI) {
		t.Fatal("valid cursor-cli CAS object changed")
	}
}

func TestOpenResumesNormalizeJob(t *testing.T) {
	dir := t.TempDir()
	store, err := cas.Open(dir + "/cas")
	if err != nil {
		t.Fatal(err)
	}
	body := transcriptLines(
		`{"type":"meta","meta":{"id":"sid-resume","cwd":"/tmp","started":"2026-09-22T16:10:00Z","version":"0.1.0"}}`,
		`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"resumed pond"}],"time":"2026-09-22T16:10:01Z"}}`,
	)
	sum := shaOf(t, body)
	if _, err := store.Put(sum, bytes.NewReader(body), int64(len(body))); err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Open(dir + "/catalog.db")
	if err != nil {
		t.Fatal(err)
	}
	ack, err := cat.Ingest(t.Context(), protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessTerva,
		NativeSessionID: "sid-resume",
		Artifacts: []protocol.Artifact{{
			Kind:    protocol.KindTranscriptJSONL,
			RelPath: "sessions/x/sid-resume.jsonl",
			Size:    int64(len(body)),
			SHA256:  sum,
		}},
	}, time.Date(2026, 9, 22, 16, 0, 0, 0, time.UTC), []catalog.Decision{{
		Relation: protocol.RelationHead, Record: true, Head: true,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cat.EnqueueNormalize(t.Context(), ack.SessionUID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := cat.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	path, err := normalize.ParquetPath(s.Parquet, "2026-09-22", protocol.HarnessTerva, ack.SessionUID)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := parquet.ReadFile[normalize.ParquetRow](path)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, row := range rows {
		if row.ContentText != nil && *row.ContentText == "resumed pond" {
			found = true
		}
	}
	if !found {
		t.Fatalf("resumed rows %+v", rows)
	}
	jobs, err := s.Catalog.ListNormalizeJobs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("job left queued: %+v", jobs)
	}
}

// A retry pushed after shutdown must not sit in the queue: no worker
// pops it, and waitIdle would wait for it forever.
func TestQueuePushAfterShutdownIsDropped(t *testing.T) {
	q := newNormalizeQueue()
	if left := q.shutdown(); left != 0 {
		t.Fatalf("empty queue left %d", left)
	}
	q.push(catalog.NormalizeJob{SessionUID: "late", Gen: 1})
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := q.waitIdle(ctx); err != nil {
		t.Fatalf("closed queue holds a pushed job: %v", err)
	}
}
