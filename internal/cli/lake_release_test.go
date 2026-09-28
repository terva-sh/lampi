package cli

import (
	"net/http/httptest"
	"strings"
	"testing"

	"terva.sh/lampi/internal/api"
)

func TestStatusSaysWhenTheAgentIsBehindItsLake(t *testing.T) {
	lake, err := api.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	lake.Allow("abc123")
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)
	defer func(f func() string) { runningRelease = f }(runningRelease)

	if got := probeRelease(srv.URL, "abc123"); got != "" {
		t.Fatalf("a lake with no release printed %q", got)
	}
	lake.Release = "v0.2.0"
	runningRelease = func() string { return "v0.1.3" }
	got := probeRelease(srv.URL, "abc123")
	if !strings.Contains(got, "lake_release: v0.2.0") || !strings.Contains(got, "runs v0.1.3, behind its lake; run terva-lampi self-update") {
		t.Fatalf("behind:\n%s", got)
	}
	runningRelease = func() string { return "v0.2.0" }
	if got := probeRelease(srv.URL, "abc123"); strings.Contains(got, "upgrade:") || !strings.Contains(got, "lake_release: v0.2.0") {
		t.Fatalf("level:\n%s", got)
	}
	// A dev build is never told to upgrade.
	runningRelease = func() string { return "0.0.0" }
	if got := probeRelease(srv.URL, "abc123"); strings.Contains(got, "upgrade:") {
		t.Fatalf("dev build:\n%s", got)
	}
}
