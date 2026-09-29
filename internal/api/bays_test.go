package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/protocol"
)

func postBays(t *testing.T, s *Server, token, native, sum string, size int64, aware bool, bays ...string) *httptest.ResponseRecorder {
	t.Helper()
	m := protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-" + token[:4],
		Harness:         protocol.HarnessTerva,
		NativeSessionID: native,
		Artifacts: []protocol.Artifact{{
			Kind: protocol.KindTranscriptJSONL, RelPath: "sessions/x/" + native + ".jsonl", Size: size, SHA256: sum,
		}},
		Bays:     bays,
		BayAware: aware,
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/manifests", bytes.NewReader(mustJSON(t, m)))
	req.Header.Set("Authorization", "Bearer "+token)
	s.Handler().ServeHTTP(rr, req)
	return rr
}

// TestManifestBaysFollowTheDevicesGrants is TKT-01M3NNF29W: a device is
// placed only in the bays it may write, is told which it asked for and
// did not get without learning why, and with the default off a session
// nothing places is refused in a way each agent generation backs off
// on.
func TestManifestBaysFollowTheDevicesGrants(t *testing.T) {
	ctx := t.Context()
	s, _, _, sum, size := devicesLake(t)
	if _, err := s.Catalog.CreateBay(ctx, "work", "admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	laptop, err := s.Catalog.DeviceByName(ctx, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Catalog.AddGrant(ctx, catalog.PrincipalDevice, laptop.ID, "work", catalog.PermWrite, "admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	work, err := s.Catalog.ResolveBay(ctx, "work")
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		token   string
		want    []string
		refused []string
	}{
		{laptopToken, []string{work.ID}, []string{"nope"}},
		{desktopToken, []string{catalog.DefaultBayID}, []string{"work", "nope"}},
	} {
		rr := postBays(t, s, tc.token, "sess-"+tc.token[:4], sum, size, true, "work", "nope")
		if rr.Code != http.StatusOK {
			t.Fatalf("post %d %s", rr.Code, rr.Body)
		}
		var ack protocol.ManifestAck
		if err := json.Unmarshal(rr.Body.Bytes(), &ack); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(ack.RefusedBays, tc.refused) {
			t.Errorf("refused %v want %v", ack.RefusedBays, tc.refused)
		}
		if strings.Contains(rr.Body.String(), catalog.RefusedNotGranted) || strings.Contains(rr.Body.String(), catalog.RefusedNoBay) {
			t.Errorf("the ack says why a bay was refused: %s", rr.Body)
		}
		got, err := s.Catalog.SessionBays(ctx, ack.SessionUID)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("bays %v err=%v want %v", got, err, tc.want)
		}
	}

	if err := s.Catalog.SetDefaultEnabled(ctx, false, "admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		aware bool
		code  int
		body  string
	}{{true, http.StatusConflict, protocol.CodeNoBay}, {false, http.StatusForbidden, ""}} {
		rr := postBays(t, s, desktopToken, "sess-off", sum, size, tc.aware, "work")
		var body protocol.ErrorBody
		_ = json.Unmarshal(rr.Body.Bytes(), &body)
		if rr.Code != tc.code || body.Code != tc.body {
			t.Errorf("bay_aware=%v: %d %s", tc.aware, rr.Code, rr.Body)
		}
	}
	if rr := postBays(t, s, laptopToken, "sess-off-ok", sum, size, true, "work"); rr.Code != http.StatusOK {
		t.Errorf("a placed session with the default off: %d %s", rr.Code, rr.Body)
	}
}

func TestManifestBaysAreBounded(t *testing.T) {
	s, _, _, sum, size := devicesLake(t)
	many := make([]string, protocol.MaxManifestBays+1)
	for i := range many {
		many[i] = "b"
	}
	for name, bays := range map[string][]string{"many": many, "empty": {""}, "long": {strings.Repeat("a", maxBayRef+1)}} {
		if rr := postBays(t, s, laptopToken, "sess-"+name, sum, size, true, bays...); rr.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, rr.Code, rr.Body)
		}
	}
}

// TestHelloListsOnlyTheDevicesWritableBays: a device learns the names
// of the bays it may write and of no other.
func TestHelloListsOnlyTheDevicesWritableBays(t *testing.T) {
	ctx := t.Context()
	s, _, _, _, _ := devicesLake(t)
	for _, name := range []string{"work", "client-secret"} {
		if _, err := s.Catalog.CreateBay(ctx, name, "admin", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	laptop, err := s.Catalog.DeviceByName(ctx, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Catalog.AddGrant(ctx, catalog.PrincipalDevice, laptop.ID, "work", catalog.PermWrite, "admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	for token, want := range map[string][]string{laptopToken: {"default", "work"}, desktopToken: {"default"}} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/hello", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		s.Handler().ServeHTTP(rr, req)
		var hello protocol.HelloResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &hello); err != nil || rr.Code != http.StatusOK {
			t.Fatalf("hello %d %s", rr.Code, rr.Body)
		}
		if !reflect.DeepEqual(hello.Bays, want) || !slices.Contains(hello.Features, protocol.FeatureBays) {
			t.Errorf("bays %v features %v want %v", hello.Bays, hello.Features, want)
		}
		if strings.Contains(rr.Body.String(), "client-secret") {
			t.Errorf("hello names a bay the device may not write: %s", rr.Body)
		}
	}
}
