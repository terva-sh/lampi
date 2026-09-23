package catalog

import (
	"testing"
	"time"

	"terva.sh/lampi/internal/protocol"
)

func TestNormalizeQueueKeepsNewestGen(t *testing.T) {
	c, err := Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	ctx := t.Context()
	ack, err := c.Ingest(ctx, sampleManifest(), time.Date(2026, 9, 22, 16, 0, 0, 0, time.UTC), []Decision{{
		Relation: protocol.RelationHead, Record: true, Head: true,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	gen1, err := c.EnqueueNormalize(ctx, ack.SessionUID, time.Date(2026, 9, 22, 16, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	gen2, err := c.EnqueueNormalize(ctx, ack.SessionUID, time.Date(2026, 9, 22, 16, 2, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if gen1 != 1 || gen2 != 2 {
		t.Fatalf("gens %d %d", gen1, gen2)
	}
	jobs, err := c.ListNormalizeJobs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].SessionUID != ack.SessionUID || jobs[0].Gen != gen2 {
		t.Fatalf("jobs %+v", jobs)
	}
	if err := c.DeleteNormalizeJob(ctx, ack.SessionUID, gen1); err != nil {
		t.Fatal(err)
	}
	jobs, err = c.ListNormalizeJobs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("stale delete removed the new job: %+v", jobs)
	}
	if err := c.DeleteNormalizeJob(ctx, ack.SessionUID, gen2); err != nil {
		t.Fatal(err)
	}
	jobs, err = c.ListNormalizeJobs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("jobs %+v", jobs)
	}
}
