// smoketest serves only synthetic data with a fake HTTPS IdP on ephemeral
// loopback ports. It is never included in the terva-lampi binary.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/protocol"
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
				err = lake.StoreEvents(ctx, ack.SessionUID, []normalize.Event{{SchemaVersion: 1, EventID: fmt.Sprint(i), SessionID: native, Harness: harness, EventType: normalize.EventMeta, RecordedAt: "2026-09-26T12:00:00Z", IngestedAt: "2026-09-26T12:00:00Z"}}, nil)
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
	idp := testidp.New()
	defer idp.Close()
	if *deny {
		idp.Groups = []string{"outsiders"}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer ln.Close()
	origin := "http://" + ln.Addr().String()
	cfg := webconfig.Config{BaseURL: origin, OIDC: webconfig.OIDC{Issuer: idp.URL(), ClientID: "lampi-smoke", RoleMap: map[string]string{"readers": "viewer"}}}
	lake.Web, err = web.New(cfg, lake.Catalog, idp.Client())
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
