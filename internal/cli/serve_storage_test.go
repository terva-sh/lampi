package cli

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"sync/atomic"
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

// TKT-01M3JV461: metrics bind loopback unless the operator opts in.
func TestCheckMetricsAddr(t *testing.T) {
	for _, c := range []struct {
		addr   string
		public bool
		ok     bool
	}{
		{"", false, true}, {"", true, false},
		{"127.0.0.1:9187", false, true}, {"[::1]:9187", false, true},
		{"0.0.0.0:9187", false, false}, {"0.0.0.0:9187", true, true},
		{":9187", false, false}, {"nonsense", false, false},
	} {
		if err := checkMetricsAddr(c.addr, c.public); (err == nil) != c.ok {
			t.Errorf("checkMetricsAddr(%q, %v) = %v", c.addr, c.public, err)
		}
	}
}

// TKT-01M3JV461: closing the lake cancels a scrape still running after
// the grace period and waits for its handler, so no scrape reads a
// closed catalog.
func TestMetricsScrapeEndsBeforeCatalogCloses(t *testing.T) {
	old := metricsGrace
	metricsGrace = 50 * time.Millisecond
	t.Cleanup(func() { metricsGrace = old })
	lake, err := api.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	var finished atomic.Bool
	var catalogOpen atomic.Bool
	catalogOpen.Store(true)
	lake.BeforeClose = func() { catalogOpen.Store(false) }
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		time.Sleep(20 * time.Millisecond)
		if !catalogOpen.Load() {
			t.Error("the scrape ran on after the catalog closed")
		}
		finished.Store(true)
	})
	serveMetrics(Env{Stdout: io.Discard, Stderr: io.Discard}, lake, ln, slow)
	go func() { _, _ = http.Get("http://" + ln.Addr().String() + "/metrics") }()
	<-started
	if err := lake.Close(); err != nil {
		t.Fatal(err)
	}
	if !finished.Load() {
		t.Fatal("Close returned before the scrape's handler finished")
	}
}
