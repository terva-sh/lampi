package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/auth"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/identity"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/regcode"
)

func postRegister(t *testing.T, s *Server, req protocol.RegisterRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(req)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodPost, protocol.RegisterPath, bytes.NewReader(body)))
	return rr
}

func TestRegisterRedeemsACodeOnceAndTheTokenWorks(t *testing.T) {
	s, dir, logs, sum, size := devicesLake(t)
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	if _, err := s.EnsureIdentity(dir); err != nil {
		t.Fatal(err)
	}
	s.SetProfiles(config.Profiles{config.DefaultProfile: {}, "ci": {Agent: config.AgentConfig{Debounce: "1s"}}})
	secret, _ := regcode.NewSecret()
	if _, err := s.Catalog.CreateRegistration(t.Context(), "newbox", regcode.HashSecret(secret), "ci", now, now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("e7", 32)
	req := protocol.RegisterRequest{Secret: secret, TokenSHA256: auth.HashToken(token), MachineID: "machine-new", Name: "New Box"}

	rr := postRegister(t, s, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("register %d %s", rr.Code, rr.Body)
	}
	var resp protocol.RegisterResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Name != "newbox" || resp.LakeID != s.Identity.LakeID || !strings.HasPrefix(resp.DeviceID, "dev_") || resp.Config == nil {
		t.Fatalf("%+v", resp)
	}
	pub, _ := identity.ParsePublic(s.Identity.Public()[0])
	if err := identity.Verify(identity.ContextAgentConfig, resp.Config, pub); err != nil {
		t.Fatal(err)
	}
	var p protocol.AgentConfigPayload
	_ = json.Unmarshal(resp.Config.Payload, &p)
	if p.Profile != "ci" || p.DeviceID != resp.DeviceID {
		t.Fatalf("registration profile %+v", p)
	}

	// The new token is a device the token file does not hold.
	stats := httptest.NewRecorder()
	sreq := httptest.NewRequest(http.MethodGet, "/v1/stats", nil)
	sreq.Header.Set("Authorization", "Bearer "+token)
	s.Handler().ServeHTTP(stats, sreq)
	if stats.Code != http.StatusOK {
		t.Fatalf("registered token: %d %s", stats.Code, stats.Body)
	}
	// It is bound to its machine at once.
	if rr := postAs(t, s, token, "machine-new", sum, size); rr.Code != http.StatusOK {
		t.Fatalf("registered device on its own machine: %d %s", rr.Code, rr.Body)
	}
	if rr := postAs(t, s, token, "machine-other", sum, size); rr.Code != http.StatusForbidden {
		t.Fatalf("registered device posted as another machine: %d %s", rr.Code, rr.Body)
	}

	// A code redeems once. The answer does not say why.
	again := postRegister(t, s, protocol.RegisterRequest{Secret: secret, TokenSHA256: strings.Repeat("0", 64), MachineID: "m2"})
	unknown := postRegister(t, s, protocol.RegisterRequest{Secret: strings.Repeat("A", 43), TokenSHA256: strings.Repeat("0", 64), MachineID: "m2"})
	if again.Code != http.StatusForbidden || unknown.Code != http.StatusForbidden || again.Body.String() != unknown.Body.String() {
		t.Fatalf("used %d %s / unknown %d %s", again.Code, again.Body, unknown.Code, unknown.Body)
	}

	// A revoked registered device stops working on its next request.
	if _, err := s.Catalog.RevokeDevice(t.Context(), "newbox", now); err != nil {
		t.Fatal(err)
	}
	stats = httptest.NewRecorder()
	s.Handler().ServeHTTP(stats, sreq)
	if stats.Code != http.StatusUnauthorized {
		t.Fatalf("revoked registered token: %d", stats.Code)
	}

	// Neither log holds the secret or the token.
	raw, _ := os.ReadFile(audit.Path(dir))
	for _, log := range []string{string(raw), logs.String()} {
		if strings.Contains(log, secret) || strings.Contains(log, token) {
			t.Fatalf("a log holds the secret or token:\n%s", log)
		}
	}
	for _, kind := range []string{`"kind":"registration.redeemed","device":"newbox"`, `"registration.refused"`, `reason=used`, `reason=unknown`, `suggested_name=new-box`} {
		if !strings.Contains(string(raw), kind) {
			t.Fatalf("audit lacks %s:\n%s", kind, raw)
		}
	}
}

func TestRegisterRefusesExpiredRevokedAndOpenLakes(t *testing.T) {
	s, dir, _, _, _ := devicesLake(t)
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	if _, err := s.EnsureIdentity(dir); err != nil {
		t.Fatal(err)
	}
	expired, _ := regcode.NewSecret()
	revoked, _ := regcode.NewSecret()
	if _, err := s.Catalog.CreateRegistration(t.Context(), "old", regcode.HashSecret(expired), "", now.Add(-48*time.Hour), now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Catalog.CreateRegistration(t.Context(), "gone", regcode.HashSecret(revoked), "", now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Catalog.RevokeRegistration(t.Context(), "gone", now); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{expired, revoked, expired} {
		if rr := postRegister(t, s, protocol.RegisterRequest{Secret: secret, TokenSHA256: strings.Repeat("1", 64), MachineID: "m"}); rr.Code != http.StatusForbidden {
			t.Fatalf("%d %s", rr.Code, rr.Body)
		}
	}
	raw, _ := os.ReadFile(audit.Path(dir))
	if !strings.Contains(string(raw), "reason=expired") || !strings.Contains(string(raw), "reason=revoked") {
		t.Fatalf("audit:\n%s", raw)
	}
	// The expiry is recorded the first time the code is presented, once.
	if n := strings.Count(string(raw), `"kind":"registration.expired","device":"old"`); n != 1 {
		t.Fatalf("%d registration.expired lines for old, want 1:\n%s", n, raw)
	}
	if strings.Contains(string(raw), expired) {
		t.Fatal("audit holds the secret")
	}
	if rr := postRegister(t, s, protocol.RegisterRequest{Secret: expired, TokenSHA256: "NOTHEX", MachineID: "m"}); rr.Code != http.StatusBadRequest {
		t.Fatalf("bad token hash: %d", rr.Code)
	}

	open := identityLakeOpen(t)
	if rr := postRegister(t, open, protocol.RegisterRequest{Secret: expired, TokenSHA256: strings.Repeat("1", 64), MachineID: "m"}); rr.Code != http.StatusConflict {
		t.Fatalf("open lake: %d %s", rr.Code, rr.Body)
	}
}

func TestRegisterAuditsEveryMalformedAttemptWithoutTheSecret(t *testing.T) {
	s, dir, _, _, _ := devicesLake(t)
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	if _, err := s.EnsureIdentity(dir); err != nil {
		t.Fatal(err)
	}
	secret, _ := regcode.NewSecret()
	if _, err := s.Catalog.CreateRegistration(t.Context(), "newbox", regcode.HashSecret(secret), "", now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	send := func(body []byte) int {
		rr := httptest.NewRecorder()
		s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodPost, protocol.RegisterPath, bytes.NewReader(body)))
		return rr.Code
	}
	valid := func(req protocol.RegisterRequest) []byte {
		body, _ := json.Marshal(req)
		return body
	}
	for _, c := range []struct {
		body   []byte
		code   int
		reason string
	}{
		{[]byte(`{"secret":"` + secret + `",`), http.StatusBadRequest, "reason=body is not a register request"},
		{[]byte(`{"secret":"` + secret + `","name":"` + strings.Repeat("x", maxRegisterBytes) + `"}`), http.StatusRequestEntityTooLarge, "reason=body too large"},
		{valid(protocol.RegisterRequest{Secret: secret, TokenSHA256: "NOTHEX", MachineID: "m"}), http.StatusBadRequest, "reason=malformed token_sha256"},
		{valid(protocol.RegisterRequest{Secret: secret, TokenSHA256: strings.Repeat("1", 64)}), http.StatusBadRequest, "reason=malformed machine_id"},
	} {
		if got := send(c.body); got != c.code {
			t.Fatalf("%s: %d, want %d", c.reason, got, c.code)
		}
		raw, _ := os.ReadFile(audit.Path(dir))
		if !strings.Contains(string(raw), `"registration.refused"`) || !strings.Contains(string(raw), c.reason) {
			t.Fatalf("audit lacks %s:\n%s", c.reason, raw)
		}
		if strings.Contains(string(raw), secret) {
			t.Fatalf("audit holds the secret:\n%s", raw)
		}
	}
	// A body that breaks off mid-read gets an error answer, not an empty 200.
	rr := httptest.NewRecorder()
	broken := io.MultiReader(strings.NewReader(`{"secret":"`+secret), iotest.ErrReader(errors.New("connection reset")))
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodPost, protocol.RegisterPath, broken))
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "request body could not be read") {
		t.Fatalf("broken body: %d %s", rr.Code, rr.Body)
	}
	if raw, _ := os.ReadFile(audit.Path(dir)); !strings.Contains(string(raw), "reason=body could not be read") || strings.Contains(string(raw), secret) {
		t.Fatalf("audit after a broken body:\n%s", raw)
	}
	// None of those spent the code.
	if rr := postRegister(t, s, protocol.RegisterRequest{Secret: secret, TokenSHA256: strings.Repeat("1", 64), MachineID: "m"}); rr.Code != http.StatusOK {
		t.Fatalf("code after refused attempts: %d %s", rr.Code, rr.Body)
	}

	// A lake that cannot register devices audits the refusal too.
	open, openDir := identityLake(t)
	if rr := postRegister(t, open, protocol.RegisterRequest{Secret: secret, TokenSHA256: strings.Repeat("1", 64), MachineID: "m"}); rr.Code != http.StatusConflict {
		t.Fatalf("open lake: %d", rr.Code)
	}
	if raw, _ := os.ReadFile(audit.Path(openDir)); !strings.Contains(string(raw), "reason=lake accepts requests without a token") {
		t.Fatalf("open lake audit:\n%s", raw)
	}
}

func identityLakeOpen(t *testing.T) *Server {
	s, _ := identityLake(t)
	return s
}

func TestRegisterSharesTheOpenRateLimit(t *testing.T) {
	s, dir, _, _, _ := devicesLake(t)
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	if _, err := s.EnsureIdentity(dir); err != nil {
		t.Fatal(err)
	}
	var last int
	for range openBurst + 1 {
		last = postRegister(t, s, protocol.RegisterRequest{Secret: strings.Repeat("A", 43), TokenSHA256: strings.Repeat("1", 64), MachineID: "m"}).Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("after the burst: %d", last)
	}
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, protocol.KeysPath, nil))
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("keys after register burst: %d", rr.Code)
	}
}
