package upload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/adapter/cursorcli"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
)

// A body over the cap is cut where the reader said, and the chunk list
// carries those lengths. A second body that shares all but one of
// those chunks puts only that one. Cuts that do not cover the body
// fall back to fixed pieces.
func TestUploadSplitUsesReaderCuts(t *testing.T) {
	lake, _ := openLake(t)
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)
	opt := allowAll(srv, t.TempDir(), t.TempDir(), "/work/app")

	pieces := [][]byte{bytes.Repeat([]byte("a"), 700), bytes.Repeat([]byte("b"), 300), bytes.Repeat([]byte("c"), 900), bytes.Repeat([]byte("d"), 500)}
	join := func(ps [][]byte) ([]byte, []int64) {
		var body []byte
		var cuts []int64
		for _, p := range ps {
			body = append(body, p...)
			cuts = append(cuts, int64(len(p)))
		}
		return body, cuts
	}
	put := func(body []byte, cuts []int64) (Result, chunkPlan) {
		t.Helper()
		sum := sha256.Sum256(body)
		d := hex.EncodeToString(sum[:])
		arts := []protocol.Artifact{{RelPath: "acp-sessions/s/store.json", Size: int64(len(body)), SHA256: d}}
		var res Result
		lists := map[string]chunkPlan{}
		if err := uploadDigests(context.Background(), srv.Client(), opt, 1024, &res, arts, map[string][]byte{d: body}, map[string][]int64{d: cuts}, lists); err != nil {
			t.Fatal(err)
		}
		return res, lists[d]
	}

	body, cuts := join(pieces)
	res, plan := put(body, cuts)
	if len(plan.Lengths) != 4 || plan.Lengths[2] != 900 || res.Missing != 4 {
		t.Fatalf("first: %+v plan %v", res, plan.Lengths)
	}

	changed := append([][]byte{}, pieces...)
	changed[1] = bytes.Repeat([]byte("B"), 350)
	body, cuts = join(changed)
	res, plan = put(body, cuts)
	if len(plan.Lengths) != 4 || res.Checked != 4 || res.Missing != 1 {
		t.Fatalf("second: %+v plan %v", res, plan.Lengths)
	}

	res, plan = put(body, []int64{10, 20})
	if len(plan.Lengths) != 3 || plan.Lengths[0] != 1024 {
		t.Fatalf("bad cuts were used: %+v plan %v", res, plan.Lengths)
	}
	if usableCuts([]int64{1024, 1}, 1025, 1024) != true || usableCuts([]int64{1025}, 1025, 1024) || usableCuts([]int64{0, 1025}, 1025, 2048) || usableCuts(nil, 0, 1) {
		t.Fatal("usableCuts")
	}
}

// prepare hands a Cursor CLI export's cuts to its work item, both for
// an export built on the pass and for one Load builds after a memo hit.
func TestPrepareCarriesCursorCLICuts(t *testing.T) {
	home := t.TempDir()
	db := filepath.Join(home, "acp-sessions", "sid-1", "store.db")
	if err := writeCursorCLIStore(db, "hello from acp"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(db), "meta.json"), []byte(`{"cwd":"/work/app"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	opt := Options{
		MachineID: "machine-1",
		StateDir:  t.TempDir(),
		Projects:  config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: "/work/app"}}},
	}
	memo := NewMemo()
	for pass := range 2 {
		memo.begin(time.Now())
		bundle, err := cursorcli.ManifestsMemo(home, "machine-1", memo.forHarness(protocol.HarnessCursorCLI), nil, 0)
		memo.end()
		if err != nil {
			t.Fatal(err)
		}
		defer bundle.Cleanup()
		if pass == 1 && (bundle.Load == nil || len(bundle.Cuts) != 0) {
			t.Fatalf("second pass was not a memo hit: load %v cuts %v", bundle.Load != nil, bundle.Cuts)
		}
		wm, q := openQueues(t, t.TempDir())
		work, _, err := prepare(context.Background(), opt, wm, q, []adapter.Bundle{bundle})
		if err != nil || len(work) != 1 {
			t.Fatalf("pass %d: work %d err %v", pass, len(work), err)
		}
		d := work[0].manifest.Artifacts[0].SHA256
		got := work[0].cuts[d]
		var total int64
		for _, n := range got {
			total += n
		}
		if len(got) == 0 || total != work[0].manifest.Artifacts[0].Size {
			t.Fatalf("pass %d: cuts %v for size %d", pass, got, work[0].manifest.Artifacts[0].Size)
		}
	}
}
