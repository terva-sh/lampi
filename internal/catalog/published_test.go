package catalog

import (
	"path/filepath"
	"terva.sh/lampi/internal/protocol"
	"testing"
	"time"
)

func TestPublishedGenerationAndHead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	c, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { c.Close() }()
	ack, err := c.Ingest(t.Context(), sampleManifest(), time.Now(), []Decision{{Relation: protocol.RelationHead, Record: true, Head: true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	uid := ack.SessionUID
	state := func(want string) {
		t.Helper()
		got, err := c.NormalizationState(t.Context(), uid)
		if err != nil || got != want {
			t.Fatalf("state %s want %s err %v", got, want, err)
		}
	}
	state("unknown")
	g1, err := c.EnqueueNormalize(t.Context(), uid, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	state("pending")
	_, head, _, _ := c.NormalizeVersion(t.Context(), uid)
	if err := c.MarkPublished(t.Context(), uid, g1, head); err != nil {
		t.Fatal(err)
	}
	state("pending")
	if err := c.DeleteNormalizeJob(t.Context(), uid, g1); err != nil {
		t.Fatal(err)
	}
	state("ready")
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	c, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	state("ready")
	if _, err := c.db.Exec(`UPDATE sessions SET head_sha256='changed' WHERE session_uid=?`, uid); err != nil {
		t.Fatal(err)
	}
	state("unknown")
	if err := c.MarkPublished(t.Context(), uid, g1, head); err != nil {
		t.Fatal(err)
	}
	state("unknown")
	g2, _ := c.EnqueueNormalize(t.Context(), uid, time.Now())
	if err := c.MarkPublished(t.Context(), uid, g1, "changed"); err != nil {
		t.Fatal(err)
	}
	_ = c.DeleteNormalizeJob(t.Context(), uid, g2)
	state("unknown")
	if err := c.SetNormalizeError(t.Context(), uid, "failed"); err != nil {
		t.Fatal(err)
	}
	state("failed")
	g3, _ := c.EnqueueNormalize(t.Context(), uid, time.Now())
	state("pending")
	_ = c.MarkPublished(t.Context(), uid, g3, "changed")
	_ = c.DeleteNormalizeJob(t.Context(), uid, g3)
	state("ready")
}
