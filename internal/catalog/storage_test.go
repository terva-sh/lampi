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
// digest once.
func TestArtifactBytes(t *testing.T) {
	c, _ := openTemp(t)
	ctx := t.Context()
	if r, u, err := c.ArtifactBytes(ctx); err != nil || r != (StorageUse{}) || u != (StorageUse{}) {
		t.Fatalf("empty: %+v %+v %v", r, u, err)
	}
	for i, row := range []struct {
		uid, rel, sha string
		size          int64
	}{{"s1", "a", "d1", 100}, {"s2", "a", "d1", 100}, {"s2", "b", "d2", 30}} {
		if _, err := c.db.Exec(`INSERT INTO artifacts(artifact_id,session_uid,kind,relpath,sha256,size) VALUES(?,?,'transcript_jsonl',?,?,?)`, i, row.uid, row.rel, row.sha, row.size); err != nil {
			t.Fatal(err)
		}
	}
	r, u, err := c.ArtifactBytes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if r != (StorageUse{Bytes: 230, Files: 3}) || u != (StorageUse{Bytes: 130, Files: 2}) {
		t.Fatalf("referenced %+v unique %+v", r, u)
	}
}
