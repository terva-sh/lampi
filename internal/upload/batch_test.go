package upload

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/outbox"
	"terva.sh/lampi/internal/protocol"
)

func TestSyncACKsEachSessionBeforeScanningTheNext(t *testing.T) {
	lake, _ := openLake(t)
	var scanned, posted atomic.Int32
	scanHook = func(string, int64) { scanned.Add(1) }
	t.Cleanup(func() { scanHook = nil })
	handler := lake.Handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/manifests" {
			n := posted.Add(1)
			if got := scanned.Load(); got != n {
				t.Errorf("prepared %d transcripts before manifest %d was ACKed", got, n)
			}
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	home := t.TempDir()
	writeClaudeCorpus(t, home, 3, 1024)
	opt := Options{ServerURL: srv.URL, Client: srv.Client(), Token: "tok", MachineID: "machine-1", ClaudeHome: home, StateDir: t.TempDir(), Projects: config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: "/work/app"}}}}
	res, err := Sync(context.Background(), opt)
	if err != nil || res.Manifests != 3 || scanned.Load() != 3 || posted.Load() != 3 {
		t.Fatalf("sync=%+v scanned=%d posted=%d err=%v", res, scanned.Load(), posted.Load(), err)
	}
}

func TestSyncBatchFailureLeavesRetryableCurrentSession(t *testing.T) {
	lake, _ := openLake(t)
	var fail atomic.Bool
	fail.Store(true)
	var scanned atomic.Int32
	scanHook = func(string, int64) { scanned.Add(1) }
	t.Cleanup(func() { scanHook = nil })
	handler := lake.Handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/manifests" && fail.Load() {
			http.Error(w, "synthetic temporary failure", http.StatusServiceUnavailable)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	home, state := t.TempDir(), t.TempDir()
	writeClaudeCorpus(t, home, 3, 1024)
	opt := Options{ServerURL: srv.URL, Client: srv.Client(), Token: "tok", MachineID: "machine-1", ClaudeHome: home, StateDir: state, Memo: NewMemo(), Projects: config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: "/work/app"}}}}
	res, err := Sync(context.Background(), opt)
	if err == nil || res.Manifests != 0 || res.Uploaded != 1 || scanned.Load() != 1 || len(res.Inventory) == 0 {
		t.Fatalf("failed batch=%+v scanned=%d err=%v", res, scanned.Load(), err)
	}
	q, err := outbox.Open(outbox.File(state))
	if err != nil {
		t.Fatal(err)
	}
	depth, err := q.Depth(context.Background())
	q.Close()
	if err != nil || depth == 0 {
		t.Fatalf("failed current session was not durable: depth=%d err=%v", depth, err)
	}
	fail.Store(false)
	res, err = Sync(context.Background(), opt)
	if err != nil || res.Manifests != 3 || res.Uploaded != 2 {
		t.Fatalf("retry lost current/unprepared sessions or resent an existing blob: %+v, %v", res, err)
	}
	assertOutboxEmpty(t, state)
}

func TestSyncBatchesKeepDeduplicationAndChunkPlans(t *testing.T) {
	for _, chunk := range []int64{0, 32} {
		t.Run(fmt.Sprint(chunk), func(t *testing.T) {
			lake, _ := openLake(t)
			srv := httptest.NewServer(lake.Handler())
			t.Cleanup(srv.Close)
			home := t.TempDir()
			for _, id := range []string{"a", "b"} {
				writeSession(t, home, id, id+".jsonl", []byte(fmt.Sprintf("{\"type\":\"meta\",\"meta\":{\"id\":%q,\"cwd\":\"/work/app\"}}\n", id)))
				writeSession(t, home, id, id+".errors.jsonl", []byte("{\"type\":\"error\",\"message\":\"identical synthetic sidecar text for both sessions\"}\n"))
			}
			opt := allowAll(srv, home, t.TempDir(), "/work/app")
			opt.ChunkBytes = chunk
			res, err := Sync(context.Background(), opt)
			if err != nil || res.Manifests != 2 || res.Checked != 3 || res.Uploaded != 3 {
				t.Fatalf("shared sidecar was checked/uploaded twice: %+v, %v", res, err)
			}
			if chunk > 0 {
				for _, id := range []string{"a", "b"} {
					_, arts, ok, err := lake.Catalog.Current(context.Background(), protocol.HarnessTerva, id)
					if err != nil || !ok || len(arts) != 2 {
						t.Fatalf("session %s missing shared chunked artifact: %v %v %v", id, arts, ok, err)
					}
				}
			}
			assertOutboxEmpty(t, opt.StateDir)
		})
	}
}

func TestSyncBatchContinuesPastNoBayAndRetriesIt(t *testing.T) {
	lake, _ := openLake(t)
	var refuse atomic.Bool
	refuse.Store(true)
	handler := lake.Handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/manifests" && refuse.CompareAndSwap(true, false) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": protocol.CodeNoBay, "error": "synthetic no bay"})
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	home := t.TempDir()
	writeClaudeCorpus(t, home, 3, 1024)
	opt := Options{ServerURL: srv.URL, Client: srv.Client(), Token: "tok", MachineID: "machine-1", ClaudeHome: home, StateDir: t.TempDir(), Memo: NewMemo(), Projects: config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: "/work/app"}}}}
	res, err := Sync(context.Background(), opt)
	if err != nil || res.Manifests != 2 || len(res.NoBay) != 1 {
		t.Fatalf("no-bay session stopped subsequent batches: %+v, %v", res, err)
	}
	res, err = Sync(context.Background(), opt)
	if err != nil || res.Manifests != 1 || res.Unchanged != 2 || res.Uploaded != 0 || len(res.NoBay) != 0 {
		t.Fatalf("no-bay retry: %+v, %v", res, err)
	}
	assertOutboxEmpty(t, opt.StateDir)
}
