package catalog

import (
	"testing"
	"time"
)

// TKT-01M3JV45Y: samples read back grouped by time, and those older
// than storageDetail are thinned to the last of each UTC day.
func TestStorageSamplesThinPastDetail(t *testing.T) {
	c, _ := openTemp(t)
	ctx := t.Context()
	if _, ok, err := c.LatestStorage(ctx); err != nil || ok {
		t.Fatalf("latest on an empty catalog: %v %v", ok, err)
	}
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	record := func(at time.Time, cas int64) {
		t.Helper()
		err := c.RecordStorage(ctx, StorageSample{At: at, Measures: map[string]StorageUse{
			"cas": {Bytes: cas, Files: 2}, MeasureFSFree: {Bytes: 1000},
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	// Three samples on each of two old days.
	for d := 0; d < 2; d++ {
		for h := 0; h < 3; h++ {
			record(day.AddDate(0, 0, d).Add(time.Duration(h)*time.Hour), int64(10*d+h))
		}
	}
	// A sample far enough on that both days are past the detail window.
	now := day.Add(storageDetail + 72*time.Hour)
	record(now.Add(-time.Hour), 99)
	record(now, 100)

	got, err := c.StorageSamples(ctx, day, now.Add(time.Nanosecond))
	if err != nil {
		t.Fatal(err)
	}
	var cas []int64
	for _, s := range got {
		if len(s.Measures) != 2 {
			t.Fatalf("sample at %s has %d measures", s.At, len(s.Measures))
		}
		cas = append(cas, s.Measures["cas"].Bytes)
	}
	want := []int64{2, 12, 99, 100}
	if len(cas) != len(want) {
		t.Fatalf("kept %v, want %v", cas, want)
	}
	for i := range want {
		if cas[i] != want[i] {
			t.Fatalf("kept %v, want %v", cas, want)
		}
	}
	latest, ok, err := c.LatestStorage(ctx)
	if err != nil || !ok || !latest.At.Equal(now) || latest.Measures["cas"].Bytes != 100 || latest.Measures[MeasureFSFree].Bytes != 1000 {
		t.Fatalf("latest: %+v %v %v", latest, ok, err)
	}
}

// TKT-01M3JV45Y: referenced counts every artifact row, unique each
// digest once. TKT-01M3N6Y5: current counts each path's current
// version, and current unique each of those digests once.
func TestArtifactBytes(t *testing.T) {
	c, _ := openTemp(t)
	ctx := t.Context()
	if u, err := c.ArtifactBytes(ctx); err != nil || u != (ArtifactUse{}) {
		t.Fatalf("empty: %+v %v", u, err)
	}
	for i, row := range []struct {
		uid, rel, sha string
		size          int64
		current       int
	}{
		{"s1", "a", "d1", 100, 1},
		{"s2", "a", "d1", 100, 1}, // a copy of s1's file
		{"s2", "b", "d2", 30, 0},  // grew into d3
		{"s2", "b", "d3", 50, 1},
	} {
		if _, err := c.db.Exec(`INSERT INTO artifacts(artifact_id,session_uid,kind,relpath,sha256,size,current) VALUES(?,?,'transcript_jsonl',?,?,?,?)`, i, row.uid, row.rel, row.sha, row.size, row.current); err != nil {
			t.Fatal(err)
		}
	}
	u, err := c.ArtifactBytes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := ArtifactUse{
		Referenced:    StorageUse{Bytes: 280, Files: 4},
		Unique:        StorageUse{Bytes: 180, Files: 3},
		Current:       StorageUse{Bytes: 250, Files: 3},
		CurrentUnique: StorageUse{Bytes: 150, Files: 2},
	}
	if u != want {
		t.Fatalf("got %+v, want %+v", u, want)
	}
}

// TKT-01M3JV45Z: every machine that posted or is bound to a device is
// listed, with its newest upload, newest head update and recent count.
func TestMachinesActivity(t *testing.T) {
	c, _ := openTemp(t)
	ctx := t.Context()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := c.db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO devices(id,name,token_sha256,source,machine_id,created_at) VALUES('d1','laptop','t1','registration','m-bound',?)`, stamp(now))
	exec(`INSERT INTO provenance(session_uid,machine_id,sha256,relpath,ingested_at) VALUES('s1','m-old','a','r',?),('s2','m-old','b','r',?)`, stamp(now.Add(-72*time.Hour)), stamp(now.Add(-48*time.Hour)))
	for _, at := range []time.Time{now.Add(-30 * time.Hour), now.Add(-time.Hour), now.Add(-time.Minute)} {
		exec(`INSERT INTO head_updates(session_uid,machine_id,harness,received_ns,old_sha256,new_sha256,old_size,new_size,relation) VALUES('s1','m-old','terva',?,'','x',0,1,'head')`, at.UnixNano())
	}
	got, err := c.MachinesActivity(ctx, AllBays(), now.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].MachineID != "m-bound" || got[1].MachineID != "m-old" {
		t.Fatalf("machines %+v", got)
	}
	if b := got[0]; !b.LastUpload.IsZero() || !b.LastUpdate.IsZero() || b.Sessions != 0 {
		t.Errorf("bound machine with no uploads %+v", b)
	}
	o := got[1]
	if !o.LastUpload.Equal(now.Add(-48*time.Hour)) || !o.LastUpdate.Equal(now.Add(-time.Minute)) || o.Sessions != 2 || o.RecentUpdates != 2 {
		t.Errorf("machine %+v", o)
	}
	if v, err := c.SchemaVersion(ctx); err != nil || v != len(migrations) {
		t.Errorf("schema %d %v", v, err)
	}
}
