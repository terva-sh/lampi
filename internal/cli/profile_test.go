package cli

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/identity"
	"terva.sh/lampi/internal/lakeprofile"
	"terva.sh/lampi/internal/protocol"
)

// profileLake is a lake with an identity and a default profile that
// allows /work/app and turns codex off.
func profileLake(t *testing.T) (*api.Server, string) {
	t.Helper()
	dir := t.TempDir()
	lake, err := api.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	if _, err := lake.EnsureIdentity(dir); err != nil {
		t.Fatal(err)
	}
	lake.SetProfiles(config.Profiles{config.DefaultProfile: {
		Harnesses: config.Harnesses{"codex": {Enabled: false}},
		Projects:  config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: "/work/app"}}},
	}})
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)
	return lake, srv.URL
}

// pinnedConfig is a config.json whose only lake, work, is pinned to id
// and has no allow rule of its own.
func pinnedConfig(url string, id *identity.Identity) string {
	k := id.Public()[0]
	return fmt.Sprintf(`{"lakes":{"work":{"server":%q,"lake_id":%q,"key_id":%q,"public_key":%q}},"agent":{"debounce":"100ms"}}`,
		url, id.LakeID, k.ID, k.PublicKey)
}

func TestAgentFetchesThePinnedProfileAndUploadsWhatItAllows(t *testing.T) {
	lake, url := profileLake(t)
	home, cfg, state, _ := agentFixture(t, url)
	writeAgentConfig(t, cfg, pinnedConfig(url, lake.Identity))
	var buf memBuf
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	env := Env{Stdout: &buf, Stderr: &buf, Getenv: agentGetenv(home, cfg, state)}
	go func() { done <- runAgentLoop(ctx, env, "", "") }()
	// Before the profile, work has no allow rule and refuses the
	// session. The profile's allow rule then admits it.
	waitOut(t, &buf, func(s string) bool {
		return strings.Contains(s, "profile default version sha256:") && strings.Contains(s, "uploaded 1")
	})
	if n, _ := lake.Catalog.Counts(t.Context()); n.Sessions != 1 {
		t.Fatalf("sessions %d\n%s", n.Sessions, buf.String())
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "harnesses or debounce changed; restart the agent") {
		t.Fatalf("machine-wide change not reported:\n%s", buf.String())
	}
	if _, err := os.Stat(filepath.Join(state, "terva-lampi", "lakes", "work", "profile.json")); err != nil {
		t.Fatal(err)
	}

	// agent config names the lake as the source of what it set.
	var out memBuf
	if err := runAgentConfig(Env{Stdout: &out, Stderr: &out, Getenv: env.Getenv}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"lake work profile=default version=sha256:",
		"allow_source=lake:work",
		"harnesses.codex: enabled=false source=lake:work",
		"agent.debounce: 100ms source=local",
		"agent.debounce_max: 30s source=default",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("agent config lacks %q:\n%s", want, out.String())
		}
	}
}

func TestAgentRefusesAProfileSignedByAnotherKeyAndKeepsItsCache(t *testing.T) {
	lake, url := profileLake(t)
	home, cfg, state, _ := agentFixture(t, url)
	getenv := agentGetenv(home, cfg, state)
	// Fetch once with the right pin, so a good copy is cached.
	writeAgentConfig(t, cfg, pinnedConfig(url, lake.Identity))
	run := func(want ...string) string {
		t.Helper()
		var buf memBuf
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- runAgentLoop(ctx, Env{Stdout: &buf, Stderr: &buf, Getenv: getenv}, "", "") }()
		waitOut(t, &buf, func(s string) bool {
			for _, w := range want {
				if !strings.Contains(s, w) {
					return false
				}
			}
			return true
		})
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(20 * time.Second):
			t.Fatal("agent did not exit")
		}
		return buf.String()
	}
	run("profile default version", "uploaded 1")

	// Now the lake signs with a key the agent did not pin: the lake at
	// that URL is not the one registered.
	other, err := identity.New(rand.Reader, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	lake.Identity = other
	out := run("profile: identity: no signature by key", "keeping cached profile sha256:")
	if strings.Contains(out, "profile default version") {
		t.Fatalf("a profile from another key was taken:\n%s", out)
	}
	// The cache still verifies against the pin and still applies.
	var cfgOut memBuf
	if err := runAgentConfig(Env{Stdout: &cfgOut, Stderr: &cfgOut, Getenv: getenv}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cfgOut.String(), "allow_source=lake:work") {
		t.Fatalf("cached profile lost:\n%s", cfgOut.String())
	}
}

func TestOneLakesProfileNeverAppliesToAnotherLake(t *testing.T) {
	lake, url := profileLake(t)
	home, cfg, state, _ := agentFixture(t, url)
	getenv := agentGetenv(home, cfg, state)
	k := lake.Identity.Public()[0]
	writeAgentConfig(t, cfg, fmt.Sprintf(`{"projects":{"deny":[{"cwd_prefix":"/work/app/secret"}]},"lakes":{
		"work":{"server":%q,"lake_id":%q,"key_id":%q,"public_key":%q},
		"home":{"server":"http://127.0.0.1:9"}}}`, url, lake.Identity.LakeID, k.ID, k.PublicKey))
	allow := config.Profile{Projects: config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: "/work"}}}}
	signed, err := lake.Identity.Sign(identity.ContextAgentConfig, protocol.AgentConfigPayload{
		LakeID: lake.Identity.LakeID, Profile: "default", Version: allow.Version(),
		Config: json.RawMessage(`{"projects":{"allow":[{"cwd_prefix":"/work"}]}}`),
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	all, err := config.ResolveLakes(config.File{Lakes: map[string]config.LakeConfig{"work": {Server: url, LakeID: lake.Identity.LakeID, KeyID: k.ID, PublicKey: k.PublicKey}}}, getenv, config.LakeFlags{})
	if err != nil {
		t.Fatal(err)
	}
	d, err := lakeprofile.Verify(signed, all[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := lakeprofile.Save(filepath.Join(state, "terva-lampi", "lakes", "work"), d); err != nil {
		t.Fatal(err)
	}
	cc, err := loadClientConfig(Env{Getenv: getenv}, io.Discard, config.LakeFlags{})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]config.Lake{}
	for _, l := range cc.lakes {
		byName[l.Name] = l
	}
	app := config.ProjectID{CWD: "/work/app", NoRepo: true}
	secret := config.ProjectID{CWD: "/work/app/secret", NoRepo: true}
	if !byName["work"].Projects.Permitted(app) || byName["work"].Projects.Permitted(secret) {
		t.Fatalf("work: %+v", byName["work"].Projects)
	}
	if byName["home"].Projects.Permitted(app) || len(byName["home"].Projects.Allow) != 0 {
		t.Fatalf("work's profile reached home: %+v", byName["home"].Projects)
	}
}

func TestAgentCachesAProfileRenamedWithTheSameContent(t *testing.T) {
	lake, url := profileLake(t)
	home, cfg, state, _ := agentFixture(t, url)
	getenv := agentGetenv(home, cfg, state)
	writeAgentConfig(t, cfg, pinnedConfig(url, lake.Identity))
	// The cache holds profile ci with the content the lake now serves
	// as default, as after serve devices set-profile moved the device.
	p := lake.Profiles()[config.DefaultProfile]
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := lake.Identity.Sign(identity.ContextAgentConfig, protocol.AgentConfigPayload{
		LakeID: lake.Identity.LakeID, Profile: "ci", Version: p.Version(), Config: raw,
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	cc, err := loadClientConfig(Env{Getenv: getenv}, io.Discard, config.LakeFlags{})
	if err != nil {
		t.Fatal(err)
	}
	d, err := lakeprofile.Verify(signed, cc.lakes[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := lakeprofile.Save(filepath.Join(state, "terva-lampi", "lakes", "work"), d); err != nil {
		t.Fatal(err)
	}

	var buf memBuf
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runAgentLoop(ctx, Env{Stdout: &buf, Stderr: &buf, Getenv: getenv}, "", "") }()
	waitOut(t, &buf, func(s string) bool {
		return strings.Contains(s, "profile default version "+p.Version()) && strings.Contains(s, "uploaded 1")
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// The content did not change, so the lake is not restarted.
	if strings.Contains(buf.String(), "reload:") {
		t.Fatalf("a renamed profile with the same content reloaded the lake:\n%s", buf.String())
	}
	var out memBuf
	if err := runAgentConfig(Env{Stdout: &out, Stderr: &out, Getenv: getenv}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "lake work profile=default version="+p.Version()) {
		t.Fatalf("cached profile keeps the old name:\n%s", out.String())
	}
}

func TestAgentHoldsUploadsUntilTheFirstProfileFetchAnswers(t *testing.T) {
	lake, _ := profileLake(t)
	lake.SetProfiles(config.Profiles{config.DefaultProfile: {
		Projects: config.Projects{Deny: []config.ProjectMatch{{CWDPrefix: "/work/app"}}},
	}})
	// The profile answers late, so an agent that pushes before it has
	// the lake's deny rule uploads first.
	h := lake.Handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/agent/config" {
			time.Sleep(time.Second)
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	home, cfg, state, _ := agentFixture(t, srv.URL)
	k := lake.Identity.Public()[0]
	// config.json allows the session's project for work; only the
	// lake's profile denies it.
	writeAgentConfig(t, cfg, fmt.Sprintf(`{"lakes":{"work":{"server":%q,"lake_id":%q,"key_id":%q,"public_key":%q,
		"projects":{"allow":[{"cwd_prefix":"/work/app"}]}}},"agent":{"debounce":"100ms"}}`,
		srv.URL, lake.Identity.LakeID, k.ID, k.PublicKey))
	var buf memBuf
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runAgentLoop(ctx, Env{Stdout: &buf, Stderr: &buf, Getenv: agentGetenv(home, cfg, state)}, "", "")
	}()
	waitOut(t, &buf, func(s string) bool {
		return strings.Contains(s, "profile default version") && strings.Contains(s, "refused")
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if n, _ := lake.Catalog.Counts(t.Context()); n.Sessions != 0 {
		t.Fatalf("uploaded %d sessions before the profile's deny rule arrived:\n%s", n.Sessions, buf.String())
	}
}

func TestAgentHoldsUploadsWhileTheFirstProfileCannotBeSaved(t *testing.T) {
	lake, url := profileLake(t)
	lake.SetProfiles(config.Profiles{config.DefaultProfile: {
		Projects: config.Projects{Deny: []config.ProjectMatch{{CWDPrefix: "/work/app"}}},
	}})
	home, cfg, state, _ := agentFixture(t, url)
	writeAgentConfig(t, cfg, pinnedConfig(url, lake.Identity))
	var buf memBuf
	env := Env{Stdout: &buf, Stderr: &buf, Getenv: agentGetenv(home, cfg, state)}
	lakes, _, _, err := loadAgentLakes(env, "", "")
	if err != nil {
		t.Fatal(err)
	}
	// A directory where the cache goes makes every save fail.
	cache := filepath.Join(lakes[0].opt.LakeStateDir, lakeprofile.FileName)
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	r := newLakeRunner(env, lakes[0])
	ctx, cancel := context.WithCancel(context.Background())
	changed := make(chan struct{}, 4)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		watchProfile(ctx, env, r, 50*time.Millisecond, func() bool {
			changed <- struct{}{}
			return true
		})
	}()
	defer func() { cancel(); <-stopped }()
	waitOut(t, &buf, func(s string) bool { return strings.Contains(s, "terva-lampi: profile: ") })
	// Several more fetches fail to save; none releases the runner.
	time.Sleep(200 * time.Millisecond)
	select {
	case <-r.ready:
		t.Fatalf("runner released without the profile's rules:\n%s", buf.String())
	default:
	}
	if !strings.Contains(buf.String(), "uploads wait until profile default version sha256:") {
		t.Fatalf("held runner not reported:\n%s", buf.String())
	}
	if err := os.Remove(cache); err != nil {
		t.Fatal(err)
	}
	select {
	case <-r.ready:
	case <-time.After(15 * time.Second):
		t.Fatalf("runner still held after the profile was saved:\n%s", buf.String())
	}
	// The saved profile asked for the reload that applies its rules.
	select {
	case <-changed:
	default:
		t.Fatalf("no reload was asked for:\n%s", buf.String())
	}
	if _, err := os.Stat(cache); err != nil {
		t.Fatal(err)
	}
}

func TestAgentHoldsUploadsUntilTheReloadForANewProfileSucceeds(t *testing.T) {
	lake, url := profileLake(t)
	lake.SetProfiles(config.Profiles{config.DefaultProfile: {
		Projects: config.Projects{Deny: []config.ProjectMatch{{CWDPrefix: "/work/app"}}},
	}})
	home, cfg, state, _ := agentFixture(t, url)
	writeAgentConfig(t, cfg, pinnedConfig(url, lake.Identity))
	var buf memBuf
	env := Env{Stdout: &buf, Stderr: &buf, Getenv: agentGetenv(home, cfg, state)}
	lakes, _, _, err := loadAgentLakes(env, "", "")
	if err != nil {
		t.Fatal(err)
	}
	r := newLakeRunner(env, lakes[0])
	ctx, cancel := context.WithCancel(context.Background())
	// The reload fails until the test lets it succeed, as when
	// config.json does not resolve.
	var ok atomic.Bool
	asked := make(chan bool, 64)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		watchProfile(ctx, env, r, 50*time.Millisecond, func() bool {
			v := ok.Load()
			asked <- v
			return v
		})
	}()
	defer func() { cancel(); <-stopped }()
	// The profile is saved once, and later fetches of the same version
	// ask for the failed reload again; none releases the runner.
	for range 3 {
		select {
		case <-asked:
		case <-time.After(15 * time.Second):
			t.Fatalf("the failed reload was not asked for again:\n%s", buf.String())
		}
	}
	select {
	case <-r.ready:
		t.Fatalf("runner released before the profile's rules were in force:\n%s", buf.String())
	default:
	}
	if !strings.Contains(buf.String(), "reload failed; uploads wait until profile default version sha256:") {
		t.Fatalf("held runner not reported:\n%s", buf.String())
	}
	ok.Store(true)
	select {
	case <-r.ready:
	case <-time.After(15 * time.Second):
		t.Fatalf("runner still held after the reload succeeded:\n%s", buf.String())
	}
	// Once a reload succeeded, fetches of the same version ask for none.
	time.Sleep(200 * time.Millisecond)
	succeeded := 0
	for len(asked) > 0 {
		if <-asked {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("reload asked for %d times after the failures, want 1", succeeded)
	}
}
