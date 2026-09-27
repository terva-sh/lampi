package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/lakeprofile"
	"terva.sh/lampi/internal/upload"
)

// lakeSet is the lakes a running agent pushes to. The watch and SIGUSR1
// wake them through it, and SIGHUP replaces them with what config.json
// says now.
type lakeSet struct {
	env   Env
	ctx   context.Context // ends on shutdown; every runner's context derives from it
	state string
	// src, serverFlag and tokenFlag are what the agent started with. A
	// reload keeps them: harness roots are watched from the start.
	src                   []source
	serverFlag, tokenFlag string
	// machine is the harnesses and debounce in force, which a reload
	// does not change. A reload that would change them says so.
	machine string

	wg sync.WaitGroup
	// reloading is held for the whole of a reload. It keeps reloads one
	// at a time, since SIGHUP and each lake's profile fetch can ask for
	// one, and wait does not return, nor agent.pid get released, while a
	// reload still works on the lakes' state.
	reloading sync.Mutex
	mu        sync.Mutex
	// runners is keyed by lake name. reload is the only writer besides
	// the start, and reloading keeps reloads one at a time.
	runners map[string]*lakeRunner
}

// start runs one lake until the set's context ends or stop is called
// for it. It moves single-lake state first when the lake is default.
func (s *lakeSet) start(l agentLake) error {
	if err := s.prepare(l); err != nil {
		return err
	}
	s.launch(l)
	return nil
}

// prepare is the part of starting a lake that can fail: moving
// single-lake state when the lake is default. Once shutdown has begun
// it does nothing, since the lake will not start.
func (s *lakeSet) prepare(l agentLake) error {
	if l.name != config.DefaultLake || s.ctx.Err() != nil {
		return nil
	}
	return migrateDefault(s.env, s.state, true)
}

// launch starts the lake's runner. It cannot fail.
func (s *lakeSet) launch(l agentLake) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Shutdown has begun: the lake would only drain an outbox the
	// runner before it already drained.
	if s.ctx.Err() != nil {
		return
	}
	r := newLakeRunner(s.env, l)
	ctx, cancel := context.WithCancel(s.ctx)
	r.stop = cancel
	s.runners[l.name] = r
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer close(r.done)
		defer cancel()
		r.run(ctx)
	}()
	// A pinned lake's profile is fetched while the lake runs, and the
	// lake does not push until the first fetch has answered. Its loop
	// is not part of done: a profile that changes asks for a reload,
	// and that reload may stop this very lake.
	if !lakeprofile.Pinned(l.cfg) {
		close(r.ready)
	} else {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			watchProfile(ctx, s.env, r, profileEvery, s.reload)
		}()
	}
}

// wait returns once a reload in progress has finished and every runner
// has drained, after the set's context has ended. A reload that begins
// later sees the context ended and does nothing.
func (s *lakeSet) wait() {
	s.reloading.Lock()
	s.reloading.Unlock()
	s.wg.Wait()
}

func (s *lakeSet) snapshot() []*lakeRunner {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*lakeRunner, 0, len(s.runners))
	for _, r := range s.runners {
		out = append(out, r)
	}
	return out
}

// wakeAll asks every lake to push. Growth skips a lake that is waiting
// out a failure: its retry picks the growth up, and a lake that is down
// is not asked again on every append. The start pass and SIGUSR1 ask
// every lake.
func (s *lakeSet) wakeAll(growth bool) {
	for _, r := range s.snapshot() {
		if growth && r.waiting.Load() {
			continue
		}
		if !growth {
			r.waiting.Store(false)
		}
		r.wake()
	}
}

// anyReady reports whether growth has a lake to wake. With no lake, or
// every lake waiting out a failure, the debouncer is not started.
func (s *lakeSet) anyReady() bool {
	for _, r := range s.snapshot() {
		if !r.waiting.Load() {
			return true
		}
	}
	return false
}

// reload reads config.json and the lake flags again and changes the
// running lakes to match. A lake that is gone, or whose server, token,
// machine id or rules changed, is stopped and drains its outbox first,
// so work already queued for it is pushed, not dropped. Then the new
// and changed lakes start with a full pass. A lake that did not change
// keeps running, its backoff and memo intact. A config that does not
// resolve, a token that would go in the clear, or a new lake that
// cannot be prepared leaves the lakes as they were.
func (s *lakeSet) reload() {
	s.reloading.Lock()
	defer s.reloading.Unlock()
	if s.ctx.Err() != nil {
		return
	}
	next, cc, err := s.resolve()
	// Shutdown began while the config was read: say nothing, and leave
	// the draining runners alone.
	if s.ctx.Err() != nil {
		return
	}
	if err != nil {
		fmt.Fprintf(s.env.stderr(), "terva-lampi: reload: %v; the lakes are unchanged\n", err)
		return
	}
	if m := machineOf(cc.file); m != s.machine {
		s.machine = m
		fmt.Fprintln(s.env.stderr(), "terva-lampi: reload: harnesses or debounce changed; restart the agent to apply them")
	}
	want := map[string]agentLake{}
	for _, l := range next {
		want[l.name] = l
	}
	s.mu.Lock()
	var removed, changed, added, kept []string
	for name, r := range s.runners {
		l, ok := want[name]
		switch {
		case !ok:
			removed = append(removed, name)
		case !sameLake(r.lake, l):
			changed = append(changed, name)
		default:
			kept = append(kept, name)
		}
	}
	for name := range want {
		if _, ok := s.runners[name]; !ok {
			added = append(added, name)
		}
	}
	s.mu.Unlock()

	// What can fail runs before any lake stops, so a failure leaves
	// every lake running as it was rather than one stopped for good.
	for _, l := range next {
		if !slices.Contains(added, l.name) && !slices.Contains(changed, l.name) {
			continue
		}
		if err := s.prepare(l); err != nil {
			fmt.Fprintf(s.env.stderr(), "terva-lampi: reload: lake %s: %v; the lakes are unchanged\n", l.name, err)
			return
		}
	}
	// Shutdown began while the lakes were prepared: the runners are
	// draining already and nothing new starts.
	if s.ctx.Err() != nil {
		return
	}

	s.mu.Lock()
	var stopping []*lakeRunner
	for name, r := range s.runners {
		if slices.Contains(kept, name) {
			r.setLabel(want[name].label)
			continue
		}
		stopping = append(stopping, r)
		delete(s.runners, name)
	}
	s.mu.Unlock()

	// A changed lake keeps its state directory, so its old runner must
	// be done with the outbox before the new one opens it.
	for _, r := range stopping {
		r.stop()
	}
	for _, r := range stopping {
		<-r.done
	}
	for _, l := range next {
		if !slices.Contains(added, l.name) && !slices.Contains(changed, l.name) {
			continue
		}
		s.launch(l)
		s.mu.Lock()
		r := s.runners[l.name]
		s.mu.Unlock()
		if r != nil {
			r.wake()
		}
	}
	fmt.Fprintln(s.env.stdout(), reloadLine(added, removed, changed, kept))
}

func (s *lakeSet) resolve() ([]agentLake, clientConfig, error) {
	cc, err := loadClientConfig(s.env, s.env.stderr(), config.LakeFlags{Server: s.serverFlag, TokenFile: s.tokenFlag})
	if err != nil {
		return nil, clientConfig{}, err
	}
	lakes, err := agentLakes(s.env, cc.file, s.src, cc.lakes)
	return lakes, cc, err
}

// machineOf is the machine-wide fields a running agent cannot change,
// as one comparable string.
func machineOf(f config.File) string {
	raw, _ := json.Marshal(struct {
		H config.Harnesses
		A config.AgentConfig
	}{f.Harnesses, f.Agent})
	return string(raw)
}

// machineFields is machineOf for the config the agent starts with.
func machineFields(env Env) string {
	cc, err := loadClientConfig(env, io.Discard, config.LakeFlags{})
	if err != nil {
		return ""
	}
	return machineOf(cc.file)
}

// profileEvery is how often a running agent fetches each pinned lake's
// profile, after the fetch at start.
const profileEvery = time.Hour

// watchProfile fetches the lake's profile now and every interval until
// ctx ends. A copy that verifies against the pin and has a new version
// replaces the cached one, and changed asks for a reload so the lake's
// project rules take effect. A copy that names another profile with the
// same content replaces the cached one without a reload. A failed fetch
// or a copy that does not verify is said once per run of failures, and
// the cached copy stays. The runner is let push once the first fetch
// has answered and any reload it asked for is done. A verified copy
// with new rules that cannot be saved has not taken effect, since the
// reload reads the cache, so the runner is held until a later fetch
// saves one.
func watchProfile(ctx context.Context, env Env, r *lakeRunner, interval time.Duration, changed func()) {
	l := r.lake
	dir := l.opt.LakeStateDir
	have, haveName := "", ""
	if d, ok, err := lakeprofile.Load(dir, l.cfg); err == nil && ok {
		have, haveName = d.Payload.Version, d.Payload.Profile
	}
	failing := false
	// unsaved is set while the newest verified copy has rules the
	// cache lacks.
	unsaved := false
	released := false
	release := func() {
		if !released && !unsaved {
			released = true
			close(r.ready)
		}
	}
	fetch := func() {
		fctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		signed, err := upload.FetchAgentConfig(fctx, l.opt)
		var d lakeprofile.Doc
		if err == nil {
			d, err = lakeprofile.Verify(signed, l.cfg)
		}
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			if !failing {
				kept := "no profile is cached"
				if have != "" {
					kept = "keeping cached profile " + have
				}
				r.errf("profile: %v; %s", err, kept)
			}
			failing = true
			return
		}
		failing = false
		if d.Payload.Version == have && d.Payload.Profile == haveName {
			unsaved = false
			return
		}
		newVersion := d.Payload.Version != have
		if err := lakeprofile.Save(dir, d); err != nil {
			if newVersion && !released {
				unsaved = true
				r.errf("profile: %v; uploads wait until profile %s version %s is saved", err, d.Payload.Profile, d.Payload.Version)
			} else {
				r.errf("profile: %v", err)
			}
			return
		}
		unsaved = false
		have, haveName = d.Payload.Version, d.Payload.Profile
		fmt.Fprintf(env.stdout(), "%sprofile %s version %s\n", r.prefix(), haveName, have)
		if newVersion {
			changed()
		}
	}
	fetch()
	release()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			fetch()
			release()
		}
	}
}

// sameLake reports whether a running lake can keep running under the
// reloaded settings. The label is not compared: it is set in place.
func sameLake(a, b agentLake) bool {
	x, y := a.opt, b.opt
	return a.tokenPath == b.tokenPath &&
		x.ServerURL == y.ServerURL && x.Token == y.Token &&
		x.MachineID == y.MachineID && x.LakeStateDir == y.LakeStateDir &&
		x.UploadHits == y.UploadHits && reflect.DeepEqual(x.Projects, y.Projects)
}

func reloadLine(added, removed, changed, kept []string) string {
	var parts []string
	for _, p := range []struct {
		verb  string
		names []string
	}{{"added", added}, {"removed", removed}, {"restarted", changed}, {"unchanged", kept}} {
		if len(p.names) > 0 {
			sort.Strings(p.names)
			parts = append(parts, p.verb+" "+strings.Join(p.names, ", "))
		}
	}
	if len(added)+len(changed)+len(kept) == 0 {
		parts = append(parts, "no lake is configured: watching, uploading nothing")
	}
	return "reload: " + strings.Join(parts, "; ")
}
