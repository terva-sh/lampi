// smoketest serves only synthetic data with a fake HTTPS IdP on ephemeral
// loopback ports. It is never included in the terva-lampi binary.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/recall"
	"terva.sh/lampi/internal/registrar"
	"terva.sh/lampi/internal/storage"
	"terva.sh/lampi/internal/testidp"
	"terva.sh/lampi/internal/web"
	"terva.sh/lampi/internal/webconfig"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	deny := flag.Bool("deny", false, "synthetic identity has no mapped group")
	empty := flag.Bool("empty", false, "empty synthetic catalog")
	operator := flag.Bool("operator", false, "synthetic identity is also an operator, and registration codes are on")
	flag.Parse()
	dir, err := os.MkdirTemp("", "lampi-web-smoke-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	lake, err := api.Open(dir)
	if err != nil {
		return err
	}
	defer lake.Close()
	lake.Allow("synthetic-device-token-used-only-by-this-fixture")
	ctx := context.Background()
	if !*empty {
		for i := 0; i < 123; i++ {
			harness := []string{"terva", "claude", "codex", "opencode", "cursor", "cursor-cli"}[i%6]
			native := fmt.Sprintf("Session %03d · synthetic %s", i, harness)
			if i == 0 {
				native = "<script>window.owned=true</script>"
			}
			body := []byte(fmt.Sprintf("synthetic session %d", i))
			digest := sha256.Sum256(body)
			sha := hex.EncodeToString(digest[:])
			if _, err := lake.CAS.Put(sha, bytes.NewReader(body), int64(len(body))); err != nil {
				return err
			}
			m := protocol.Manifest{CaptureProtocol: protocol.Version, MachineID: fmt.Sprintf("synthetic-machine-%d", i%3), Harness: harness, NativeSessionID: native, Project: protocol.Project{CWD: fmt.Sprintf("/synthetic/project-%d", i%4)}, Artifacts: []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: "session.jsonl", SHA256: sha, Size: int64(len(body))}}}
			relation := protocol.RelationHead
			if i%17 == 0 {
				relation = protocol.RelationDivergentCopy
			}
			ack, err := lake.Catalog.Ingest(ctx, m, time.Now().Add(time.Duration(-i)*time.Minute), []catalog.Decision{{Relation: relation, Record: true, Head: true}}, nil)
			if err != nil {
				return err
			}
			switch i % 4 {
			case 0:
				err = lake.StoreEvents(ctx, ack.SessionUID, transcript(i, native, harness), nil)
			case 1:
				_, err = lake.Catalog.EnqueueNormalize(ctx, ack.SessionUID, time.Now())
			case 2:
				err = lake.Catalog.SetNormalizeError(ctx, ack.SessionUID, "synthetic projection failure")
			}
			if err != nil {
				return err
			}
		}
	}
	if !*empty {
		if err := seedActivity(filepath.Join(dir, "catalog.db"), time.Now()); err != nil {
			return err
		}
	}
	idp := testidp.New()
	defer idp.Close()
	if *deny {
		idp.Groups = []string{"outsiders"}
	}
	if *operator {
		idp.Groups = []string{"readers", "admins"}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer ln.Close()
	origin := "http://" + ln.Addr().String()
	cfg := webconfig.Config{BaseURL: origin, OIDC: webconfig.OIDC{Issuer: idp.URL(), ClientID: "lampi-smoke", RoleMap: map[string]string{"readers": "viewer", "admins": "operator"}}}
	reader := recall.NewReader(lake.Catalog, lake.Normalized)
	index, err := recall.OpenIndex(filepath.Join(dir, recall.IndexFile), reader)
	if err != nil {
		return err
	}
	defer index.Close()
	if err := index.Pass(ctx); err != nil {
		return err
	}
	var reg *web.Registrations
	if *operator {
		// The lake's public URL is the smoke origin, so minting's key-list
		// check reaches this process.
		if _, err := lake.EnsureIdentity(dir); err != nil {
			return err
		}
		if err := lake.Catalog.SetPublicURL(ctx, origin); err != nil {
			return err
		}
		reg = &web.Registrations{Lake: func() registrar.Lake {
			return registrar.Lake{Catalog: lake.Catalog, Identity: lake.Identity(), Dir: dir}
		}, Release: "v0.1.1"}
	}
	if !*empty {
		if err := seedStorage(ctx, lake, time.Now()); err != nil {
			return err
		}
		if err := seedDevices(ctx, lake, time.Now()); err != nil {
			return err
		}
	}
	started := time.Now().Add(-26 * time.Hour)
	ops := &web.Operations{Version: "v0.0.0-smoke", Release: "v0.2.0", Started: started, LakeID: func() string { return "lake_synthetic_smoke" }, Contacts: lake.Contacts}
	lake.Web, err = web.New(cfg, lake.Catalog, reader, index, reg, ops, idp.Client())
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: lake.Handler(), ReadHeaderTimeout: 5 * time.Second}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	// Only fixture URLs are printed, never cookie values or device credentials.
	_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"url": origin, "issuer": idp.URL()})
	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-signals.Done():
	case err := <-done:
		return err
	}
	shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shut)
}

// transcript is a synthetic conversation with the event kinds the
// viewer renders differently: messages, tool calls and results, a tool
// error, usage with unknown counts, long text and opaque content.
func transcript(n int, native, harness string) []normalize.Event {
	str := func(s string) *string { return &s }
	num := func(v int) *int { return &v }
	yes, no := true, false
	var out []normalize.Event
	add := func(actor, kind, text string, edit func(*normalize.Event)) {
		i := len(out)
		ev := normalize.Event{SchemaVersion: 1, EventID: fmt.Sprintf("%d-%d", n, i), SessionID: native, Harness: harness, Actor: actor, EventType: kind,
			RecordedAt: time.Date(2026, 9, 26, 12, 0, i, 0, time.UTC).Format(time.RFC3339), IngestedAt: "2026-09-26T13:00:00Z", Redaction: normalize.Redaction{Status: "none"}}
		if text != "" {
			ev.ContentText = str(text)
		}
		if edit != nil {
			edit(&ev)
		}
		out = append(out, ev)
	}
	for turn := 0; turn < 40; turn++ {
		add(normalize.ActorUser, normalize.EventMessage, fmt.Sprintf("Turn %d: please check the synthetic build and run git push --dry-run.", turn), nil)
		add(normalize.ActorAssistant, normalize.EventMessage, "I will run the build first.\n\n```sh\ngo test ./...\n```", func(e *normalize.Event) { e.Model.ID = str("synthetic-model") })
		call := fmt.Sprintf("call-%d", turn)
		add(normalize.ActorAssistant, normalize.EventToolCall, `{"command":"git push --dry-run origin main"}`, func(e *normalize.Event) { e.Tool = normalize.Tool{Name: str("Bash"), CallID: str(call)} })
		failed := turn%7 == 3
		result := "Everything up-to-date"
		if failed {
			result = "fatal: synthetic remote rejected <refs/heads/main>"
		}
		add(normalize.ActorTool, normalize.EventToolResult, result, func(e *normalize.Event) {
			e.Tool = normalize.Tool{Name: str("Bash"), CallID: str(call), IsError: map[bool]*bool{true: &yes, false: &no}[failed]}
		})
		add(normalize.ActorHarness, normalize.EventUsage, "", func(e *normalize.Event) {
			e.Usage = normalize.Usage{Input: num(1200 + turn), Output: num(300), CacheRead: num(0)}
		})
		if turn == 5 {
			add(normalize.ActorAssistant, normalize.EventMessage, strings.Repeat("A very long synthetic answer line. ", 2000), nil)
			add(normalize.ActorAssistant, normalize.EventMessage, "Reasoning is stored encrypted.", func(e *normalize.Event) {
				e.Extra = map[string]any{"encrypted_content": "c3ludGhldGljLWNpcGhlcnRleHQ="}
			})
			add(normalize.ActorHarness, normalize.EventCompaction, "Context compacted: synthetic summary of turns 0-5.", nil)
		}
	}
	return out
}

// seedActivity gives the Activity page three weeks of synthetic history:
// recording starts 21 days back, and each day has a varying number of
// accepted updates, some of them snapshot rewrites that shrank. It
// writes head_updates directly so the sessions, their states and every
// count the rest of the smoke asserts stay as they are. A fixture only;
// the lake records these rows itself at ingest.
func seedActivity(path string, now time.Time) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	since := now.Add(-21 * 24 * time.Hour)
	if _, err := db.Exec(`UPDATE lake_meta SET value = ? WHERE key = 'head_updates_since'`, since.UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	harnesses := []string{"terva", "claude", "codex", "opencode", "cursor", "cursor-cli"}
	for d := 0; d < 21; d++ {
		n := 6 + (d*7)%11
		if d%7 == 5 || d%7 == 6 {
			n = d % 3
		}
		for k := 0; k < n; k++ {
			at := since.Add(time.Duration(d)*24*time.Hour + time.Duration(k*97%1440)*time.Minute)
			old, grown := int64(4096*(k+1)), int64(4096*(k+1)+1500*(d%5+1))
			rel := protocol.RelationGrownFrom
			if k == 0 && d%4 == 2 {
				old, grown, rel = 90000, 30000, protocol.RelationHead
			}
			if _, err := db.Exec(`INSERT INTO head_updates (session_uid, machine_id, harness, received_ns, old_sha256, new_sha256, old_size, new_size, relation)
				VALUES (?, ?, ?, ?, 'synthetic-old', 'synthetic-new', ?, ?, ?)`,
				fmt.Sprintf("synthetic-activity-%d", k), fmt.Sprintf("synthetic-machine-%d", k%3), harnesses[(d+k)%6], at.UnixNano(), old, grown, rel); err != nil {
				return err
			}
		}
	}
	return nil
}

// seedStorage records 90 days of synthetic storage samples, hourly for
// the last 14 days and daily before that, growing toward the real one
// it takes last, so the operations charts have a history to draw.
func seedStorage(ctx context.Context, lake *api.Server, now time.Time) error {
	real, err := lake.SampleStorage(ctx)
	if err != nil {
		return err
	}
	const fsTotal = 200 << 30
	for h := 90 * 24; h > 0; h-- {
		if h > 14*24 && h%24 != 0 {
			continue
		}
		at := now.Add(-time.Duration(h) * time.Hour)
		f := 1 - float64(h)/(90*24)
		m := map[string]catalog.StorageUse{}
		var total int64
		for name, u := range real.Measures {
			scaled := catalog.StorageUse{Bytes: int64(float64(u.Bytes)*f*0.6 + float64(u.Bytes)*0.4), Files: int64(float64(u.Files) * f)}
			m[name] = scaled
		}
		for _, c := range storage.Components {
			total += m[c].Bytes
		}
		// A filesystem other things share, filling slowly.
		m[catalog.MeasureFSTotal] = catalog.StorageUse{Bytes: fsTotal}
		m[catalog.MeasureFSFree] = catalog.StorageUse{Bytes: fsTotal - (60 << 30) - int64(f*float64(40<<30)) - total}
		if err := lake.Catalog.RecordStorage(ctx, catalog.StorageSample{At: at, Measures: m}); err != nil {
			return err
		}
	}
	_, err = lake.SampleStorage(ctx)
	return err
}

// seedDevices adds devices whose reports cover each state the devices
// page shows: behind, current, unstamped with local rules and an error,
// and one that has never reported.
func seedDevices(ctx context.Context, lake *api.Server, now time.Time) error {
	h := func(b string) string { return strings.Repeat(b, 64) }
	created, err := lake.Catalog.SyncTokenFile(ctx, []catalog.TokenEntry{
		{Hash: h("1"), Name: "laptop"}, {Hash: h("2"), Name: "desktop"}, {Hash: h("3"), Name: "ci-runner"}, {Hash: h("4"), Name: "spare"},
	}, now)
	if err != nil {
		return err
	}
	eff, err := lake.Catalog.ResolveProfile(ctx, created[0])
	if err != nil {
		return err
	}
	reports := []protocol.AgentReport{
		{AgentVersion: "v0.1.3", ProfileVersion: eff.Version, AllowSource: "lake default", DenySource: "none", Inventory: "sociable",
			LastSync: &protocol.AgentSyncReport{At: now.Add(-3 * time.Minute), Checked: 240, Uploaded: 12, Manifests: 4, Refused: 221, Unchanged: 7}},
		{AgentVersion: "v0.2.0", ProfileVersion: eff.Version, AllowSource: "lake default", DenySource: "local+lake default", Inventory: "strict",
			LastSync: &protocol.AgentSyncReport{At: now.Add(-40 * time.Second), Checked: 18, Uploaded: 1, Unchanged: 17}},
		{AgentVersion: "0.0.0", ProfileVersion: "sha256:0000000000000000", AllowSource: "local", DenySource: "local",
			LastError: "upload: POST /v1/hello: 503 Service Unavailable", LastErrorAt: now.Add(-2 * time.Hour)},
	}
	for i, rep := range reports {
		if err := lake.Catalog.PutDeviceReport(ctx, created[i].ID, rep, now.Add(-time.Duration(i)*time.Minute)); err != nil {
			return err
		}
	}
	return nil
}
