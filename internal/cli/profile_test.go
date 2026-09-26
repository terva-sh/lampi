package cli

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
	signed, err := lake.Identity.Sign(identity.ContextAgentConfig, protocol.AgentConfigPayload{
		LakeID: lake.Identity.LakeID, Profile: "default", Version: "v1",
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
