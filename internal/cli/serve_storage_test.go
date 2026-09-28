package cli

import (
	"bytes"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
)

// TKT-01M3JV45Y: serve samples storage at start, keeps sampling on its
// interval, and stops the sampler before the catalog closes.
func TestSampleStorageRunsUntilClose(t *testing.T) {
	lake, err := api.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ran := false
	lake.BeforeClose = func() { ran = true }
	var stderr bytes.Buffer
	sampleStorage(Env{Stdout: &stderr, Stderr: &stderr}, lake, 10*time.Millisecond)
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := lake.Catalog.StorageSamples(t.Context(), time.Time{}, time.Now().Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if len(got) >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d samples after 5s; stderr: %s", len(got), stderr.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := lake.Close(); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Error("the earlier BeforeClose did not run")
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr: %s", stderr.String())
	}
}
