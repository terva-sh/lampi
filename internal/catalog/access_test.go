package catalog

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"terva.sh/lampi/internal/protocol"
)

var headDecision = []Decision{{Relation: protocol.RelationHead, Record: true, Head: true}}

// Ingest reads the session before it writes. As a deferred transaction
// that read pins a snapshot, and once another connection commits a
// write the upgrade fails with SQLITE_BUSY without waiting. BEGIN
// IMMEDIATE waits on busy_timeout for the writer instead.
func TestIngestWaitsForAnotherWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	ctx := context.Background()
	now := time.Date(2026, 9, 22, 16, 0, 0, 0, time.UTC)
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { first.Close() })
	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { second.Close() })
	if _, err := first.Ingest(ctx, sampleManifest(), now, headDecision, nil); err != nil {
		t.Fatal(err)
	}

	tx, err := first.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET ingested_at = ?`, "writer"); err != nil {
		t.Fatal(err)
	}
	committed := make(chan error, 1)
	go func() {
		time.Sleep(200 * time.Millisecond)
		committed <- tx.Commit()
	}()

	m := sampleManifest()
	m.MachineID = "machine-b"
	if _, err := second.Ingest(ctx, m, now.Add(time.Minute), headDecision, nil); err != nil {
		t.Fatalf("ingest while another connection writes: %v", err)
	}
	if err := <-committed; err != nil {
		t.Fatal(err)
	}
}

func TestReadOnlyCatalogReadsAndDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.db")
	ctx := context.Background()
	now := time.Date(2026, 9, 22, 16, 0, 0, 0, time.UTC)
	if _, err := OpenReadOnly(path); err == nil {
		t.Fatal("read-only open created a catalog")
	}
	rw, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rw.Close() })
	ack, err := rw.Ingest(ctx, sampleManifest(), now, headDecision, nil)
	if err != nil {
		t.Fatal(err)
	}

	ro, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ro.Close() })
	if _, ok, err := ro.Session(ctx, ack.SessionUID); err != nil || !ok {
		t.Fatalf("read-only session ok=%v err=%v", ok, err)
	}
	if err := ro.SetNormalizeError(ctx, ack.SessionUID, "x"); err == nil {
		t.Fatal("read-only catalog accepted a write")
	}

	// A copy taken through the read-only handle holds what was committed.
	dest := filepath.Join(dir, "copy.db")
	if err := ro.VacuumInto(ctx, dest); err != nil {
		t.Fatal(err)
	}
	cp, err := OpenReadOnly(dest)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cp.Close() })
	n, err := cp.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n.Sessions != 1 || n.Artifacts != 1 {
		t.Fatalf("copy counts %+v", n)
	}
	if err := ro.VacuumInto(ctx, dest); err == nil {
		t.Fatal("vacuum into an existing file succeeded")
	}
}
