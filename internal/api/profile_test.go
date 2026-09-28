package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/identity"
	"terva.sh/lampi/internal/protocol"
)

func getAgentConfig(t *testing.T, s *Server, token string) (*httptest.ResponseRecorder, protocol.AgentConfigPayload) {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, protocol.AgentConfigPath, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	s.Handler().ServeHTTP(rr, req)
	var p protocol.AgentConfigPayload
	if rr.Code != http.StatusOK {
		return rr, p
	}
	var signed protocol.Signed
	if err := json.Unmarshal(rr.Body.Bytes(), &signed); err != nil {
		t.Fatal(err)
	}
	pub, err := identity.ParsePublic(s.Identity().Public()[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.Verify(identity.ContextAgentConfig, &signed, pub); err != nil {
		t.Fatal(err)
	}
	// A signature for another use does not pass as this one.
	if identity.Verify(identity.ContextHello, &signed, pub) == nil {
		t.Fatal("agent config verifies as a hello")
	}
	if err := json.Unmarshal(signed.Payload, &p); err != nil {
		t.Fatal(err)
	}
	return rr, p
}

func TestAgentConfigIsTheDevicesProfileSigned(t *testing.T) {
	s, dir, _, _, _ := devicesLake(t)
	s.Now = func() time.Time { return time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC) }
	if _, err := s.EnsureIdentity(dir); err != nil {
		t.Fatal(err)
	}
	s.SetProfiles(config.Profiles{
		config.DefaultProfile: {Agent: config.AgentConfig{Debounce: "3s"}},
		"ci":                  {Projects: config.Projects{Deny: []config.ProjectMatch{{CWDPrefix: "/secret"}}}},
	})
	if rr, _ := getAgentConfig(t, s, ""); rr.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", rr.Code)
	}
	rr, p := getAgentConfig(t, s, laptopToken)
	if rr.Code != http.StatusOK || rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("laptop: %d %s", rr.Code, rr.Body)
	}
	got, err := config.ParseProfile(p.Config)
	if err != nil {
		t.Fatal(err)
	}
	if p.LakeID != s.Identity().LakeID || p.Profile != config.DefaultProfile || got.Agent.Debounce != "3s" || p.Version != got.Version() || p.DeviceID == "" {
		t.Fatalf("laptop payload %+v", p)
	}

	if _, err := s.Catalog.SetDeviceProfile(t.Context(), "laptop", "ci", "ci", "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	_, p = getAgentConfig(t, s, laptopToken)
	if p.Profile != "ci" {
		t.Fatalf("after set-profile: %+v", p)
	}
	_, other := getAgentConfig(t, s, desktopToken)
	if other.Profile != config.DefaultProfile {
		t.Fatalf("desktop took laptop's profile: %+v", other)
	}

	// A profile that left the file is 404; the agent keeps its copy.
	s.SetProfiles(config.Profiles{config.DefaultProfile: {}})
	if rr, _ := getAgentConfig(t, s, laptopToken); rr.Code != http.StatusNotFound {
		t.Fatalf("missing profile: %d", rr.Code)
	}
}

func TestAgentConfigWithoutIdentityIs404(t *testing.T) {
	s, _, _, _, _ := devicesLake(t)
	if rr, _ := getAgentConfig(t, s, laptopToken); rr.Code != http.StatusNotFound {
		t.Fatalf("%d", rr.Code)
	}
}
