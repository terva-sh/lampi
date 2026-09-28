package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/lakeprofile"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/upload"
)

func TestAgentFetchesAProfileEditWithinSeconds(t *testing.T) {
	lake, url := profileLake(t)
	// The lake refuses the session until an edit allows it.
	putDefaultProfile(t, lake, config.Profile{Projects: config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: "/elsewhere"}}}})
	home, cfg, state, _ := agentFixture(t, url)
	writeAgentConfig(t, cfg, pinnedConfig(url, lake.Identity()))
	defer func(d time.Duration) { reportEvery = d }(reportEvery)
	reportEvery = 100 * time.Millisecond
	var buf memBuf
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	env := Env{Stdout: &buf, Stderr: &buf, Getenv: agentGetenv(home, cfg, state)}
	go func() { done <- runAgentLoop(ctx, env, "", "") }()
	waitOut(t, &buf, func(s string) bool { return strings.Contains(s, "refused 1") })

	// The hourly fetch is far off: only the header can bring the edit.
	edited := time.Now()
	putDefaultProfile(t, lake, config.Profile{Projects: config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: "/work/app"}}}})
	waitOut(t, &buf, func(s string) bool { return strings.Contains(s, "uploaded 1") })
	if took := time.Since(edited); took > 5*time.Second {
		t.Fatalf("the edit took %s to reach the agent", took)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestLakeNamesTheDevicesProfileVersionOnEachAnswer(t *testing.T) {
	lake, url := profileLake(t)
	var got []string
	opt := upload.Options{ServerURL: url, ProfileVersion: func(v string) { got = append(got, v) }}
	if _, err := upload.PostReport(t.Context(), opt, protocol.AgentReport{}); err != nil {
		t.Fatal(err)
	}
	p, err := lake.Catalog.ResolveProfile(t.Context(), catalog.Device{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != p.Version {
		t.Fatalf("header %v, want %s", got, p.Version)
	}
}

func TestAgentThatStartsWithTheLakeDownUsesItsCachedProfile(t *testing.T) {
	lake, direct := profileLake(t)
	// The lake answers nothing at first, then comes back without its
	// profile or key endpoints. Only the cached profile allows /work/app.
	var up atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up.Load() || r.URL.Path == protocol.AgentConfigPath || r.URL.Path == protocol.KeysPath {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		lake.Handler().ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	home, cfg, state, _ := agentFixture(t, srv.URL)
	writeAgentConfig(t, cfg, pinnedConfig(srv.URL, lake.Identity()))
	env := Env{Stdout: &memBuf{}, Stderr: &memBuf{}, Getenv: agentGetenv(home, cfg, state)}
	lakes, _, _, err := loadAgentLakes(env, "", "")
	if err != nil {
		t.Fatal(err)
	}
	signed, err := upload.FetchAgentConfig(t.Context(), upload.Options{ServerURL: direct})
	if err != nil {
		t.Fatal(err)
	}
	d, err := lakeprofile.Verify(signed, lakes[0].cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := lakeprofile.Save(filepath.Join(state, "terva-lampi", "lakes", "work"), d); err != nil {
		t.Fatal(err)
	}

	var buf memBuf
	env.Stdout, env.Stderr = &buf, &buf
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runAgentLoop(ctx, env, "", "") }()
	waitOut(t, &buf, func(s string) bool { return strings.Contains(s, "keeping cached profile") })
	up.Store(true)
	waitOut(t, &buf, func(s string) bool { return strings.Contains(s, "uploaded 1") })
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
