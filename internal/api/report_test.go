package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"terva.sh/lampi/internal/protocol"
)

func postReport(t *testing.T, s *Server, token string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, protocol.AgentReportPath, bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	s.Handler().ServeHTTP(rr, req)
	return rr
}

func TestAgentReportIsStoredForTheCallingDevice(t *testing.T) {
	s, _, _, _, _ := devicesLake(t)
	now := time.Date(2026, 9, 28, 15, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	ctx := t.Context()

	rep := protocol.AgentReport{
		AgentVersion:   "v0.1.3",
		ProfileVersion: "sha256:8427a42989cbc15f",
		AllowSource:    "lake default",
		LastSync:       &protocol.AgentSyncReport{At: now.Add(-time.Minute), Checked: 221, Refused: 221},
		LastError:      strings.Repeat("é", maxReportError),
	}
	body, _ := json.Marshal(rep)
	rr := postReport(t, s, laptopToken, body)
	if rr.Code != http.StatusOK {
		t.Fatalf("report %d %s", rr.Code, rr.Body)
	}
	var resp protocol.AgentReportResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil || !resp.ReceivedAt.Equal(now) {
		t.Fatalf("response %s %v", rr.Body, err)
	}

	laptop, err := s.Catalog.DeviceByName(ctx, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Catalog.DeviceReport(ctx, laptop.ID)
	if err != nil || !ok {
		t.Fatalf("stored %v %v", ok, err)
	}
	if !got.Received.Equal(now) || got.Report.AllowSource != "lake default" || got.Report.LastSync.Refused != 221 {
		t.Fatalf("stored %+v", got)
	}
	// A long error is cut to its cap on a rune boundary.
	if e := got.Report.LastError; len(e) > maxReportError || !utf8.ValidString(e) || e == "" {
		t.Fatalf("error %d bytes, valid %v", len(e), utf8.ValidString(e))
	}
	desktop, err := s.Catalog.DeviceByName(ctx, "desktop")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Catalog.DeviceReport(ctx, desktop.ID); ok {
		t.Fatal("the laptop's report was filed under the desktop")
	}
}

func TestAgentReportRefusals(t *testing.T) {
	s, _, _, _, _ := devicesLake(t)
	if rr := postReport(t, s, "", []byte(`{}`)); rr.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", rr.Code)
	}
	if rr := postReport(t, s, laptopToken, []byte(`{"agent_version":`)); rr.Code != http.StatusBadRequest {
		t.Fatalf("bad json: %d", rr.Code)
	}
	big := []byte(`{"last_error":"` + strings.Repeat("x", int(maxReportBytes)) + `"}`)
	if rr := postReport(t, s, laptopToken, big); rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("over cap: %d", rr.Code)
	}
	// A field this lake does not know is accepted and not kept.
	if rr := postReport(t, s, laptopToken, []byte(`{"agent_version":"v9","from_the_future":true}`)); rr.Code != http.StatusOK {
		t.Fatalf("unknown field: %d %s", rr.Code, rr.Body)
	}
}
