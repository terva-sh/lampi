package lakeprofile

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/identity"
	"terva.sh/lampi/internal/protocol"
)

var now = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

func lake(t *testing.T) (*identity.Identity, config.Lake) {
	t.Helper()
	id, err := identity.New(rand.Reader, now)
	if err != nil {
		t.Fatal(err)
	}
	k := id.Public()[0]
	return id, config.Lake{Name: "work", LakeID: id.LakeID, KeyID: k.ID, PublicKey: k.PublicKey}
}

// sign signs cfg for the lake with the version the lake would give it.
func sign(t *testing.T, id *identity.Identity, lakeID, cfg string) *protocol.Signed {
	t.Helper()
	var p config.Profile
	if err := json.Unmarshal([]byte(cfg), &p); err != nil {
		t.Fatal(err)
	}
	return signPayload(t, id, protocol.AgentConfigPayload{
		LakeID: lakeID, Profile: "default", Version: p.Version(), IssuedAt: now, Config: json.RawMessage(cfg),
	})
}

func signPayload(t *testing.T, id *identity.Identity, p protocol.AgentConfigPayload) *protocol.Signed {
	t.Helper()
	s, err := id.Sign(identity.ContextAgentConfig, p, now)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestVerify(t *testing.T) {
	id, l := lake(t)
	d, err := Verify(sign(t, id, id.LakeID, `{"agent":{"debounce":"2s"}}`), l)
	if err != nil || d.Profile.Agent.Debounce != "2s" {
		t.Fatalf("%+v %v", d, err)
	}

	other, _ := lake(t)
	for name, tc := range map[string]struct {
		s    *protocol.Signed
		l    config.Lake
		want string
	}{
		"another key":       {sign(t, other, id.LakeID, `{}`), l, "no signature by key"},
		"another lake id":   {sign(t, id, other.LakeID, `{}`), l, "pinned lake is"},
		"harness root":      {sign(t, id, id.LakeID, `{"harnesses":{"codex":{"root":"/etc"}}}`), l, "harness root"},
		"upload hits":       {sign(t, id, id.LakeID, `{"redaction":{"upload_hits":true}}`), l, "flagged files"},
		"field outside set": {sign(t, id, id.LakeID, `{"server":"https://x.example"}`), l, "unknown field"},
		"not pinned":        {sign(t, id, id.LakeID, `{}`), config.Lake{Name: "work"}, "no pinned key"},
		"hello context":     {helloSigned(t, id), l, "does not verify"},
	} {
		if _, err := Verify(tc.s, tc.l); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}

	// A payload changed after signing does not verify.
	s := sign(t, id, id.LakeID, `{}`)
	s.Payload = json.RawMessage(strings.Replace(string(s.Payload), `"default"`, `"ci"`, 1))
	if _, err := Verify(s, l); err == nil {
		t.Fatal("tampered payload verified")
	}
}

// The envelope around the profile is read leniently: an agent accepts
// layers, and a field a newer lake adds, while the profile itself stays
// strict.
func TestVerifyAcceptsLayersAndNewEnvelopeFields(t *testing.T) {
	id, l := lake(t)
	p := config.Profile{Agent: config.AgentConfig{Debounce: "2s"}}
	s, err := id.Sign(identity.ContextAgentConfig, map[string]any{
		"lake_id": id.LakeID, "profile": "default", "version": p.Version(), "issued_at": now,
		"config": json.RawMessage(`{"agent":{"debounce":"2s"}}`),
		"layers": []string{"profile:default", "device:laptop"}, "from_a_newer_lake": true,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	d, err := Verify(s, l)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(d.Payload.Layers, ",") != "profile:default,device:laptop" || d.Profile.Agent.Debounce != "2s" {
		t.Fatalf("verified %+v", d.Payload)
	}
}

func TestVerifyRefusesAProfileSignedForAnotherDevice(t *testing.T) {
	id, l := lake(t)
	l.DeviceID = "dev_mine"
	cfg := `{"agent":{"debounce":"2s"}}`
	p := config.Profile{Agent: config.AgentConfig{Debounce: "2s"}}
	payload := func(device string) protocol.AgentConfigPayload {
		return protocol.AgentConfigPayload{LakeID: id.LakeID, DeviceID: device, Profile: "default", Version: p.Version(), IssuedAt: now, Config: json.RawMessage(cfg)}
	}
	if _, err := Verify(signPayload(t, id, payload("dev_mine")), l); err != nil {
		t.Fatalf("own device: %v", err)
	}
	for _, device := range []string{"dev_other", ""} {
		if _, err := Verify(signPayload(t, id, payload(device)), l); err == nil || !strings.Contains(err.Error(), "this machine is device dev_mine") {
			t.Errorf("device %q: %v", device, err)
		}
	}

	// A cached copy signed for another device is not used either.
	dir := t.TempDir()
	l.DeviceID = ""
	d, err := Verify(signPayload(t, id, payload("dev_other")), l)
	if err != nil {
		t.Fatalf("entry without a device id: %v", err)
	}
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	l.DeviceID = "dev_mine"
	if _, ok, err := Load(dir, l); ok || err == nil || !strings.Contains(err.Error(), "this machine is device") {
		t.Fatalf("cached copy for another device: %v %v", ok, err)
	}
}

func TestVerifyRefusesAVersionThatDoesNotNameTheConfiguration(t *testing.T) {
	id, l := lake(t)
	stale := config.Profile{}.Version()
	s := signPayload(t, id, protocol.AgentConfigPayload{
		LakeID: id.LakeID, Profile: "default", Version: stale, IssuedAt: now, Config: json.RawMessage(`{"agent":{"debounce":"2s"}}`),
	})
	if _, err := Verify(s, l); err == nil || !strings.Contains(err.Error(), "does not match its configuration") {
		t.Fatalf("fetched: %v", err)
	}

	// The same document in the cache is refused too.
	dir := t.TempDir()
	if err := Save(dir, Doc{Signed: s}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := Load(dir, l); ok || err == nil || !strings.Contains(err.Error(), "does not match its configuration") {
		t.Fatalf("cached: %v %v", ok, err)
	}
}

// helloSigned is a hello proof's signature moved onto a profile payload.
func helloSigned(t *testing.T, id *identity.Identity) *protocol.Signed {
	t.Helper()
	p := sign(t, id, id.LakeID, `{}`)
	h, err := id.Sign(identity.ContextHello, json.RawMessage(p.Payload), now)
	if err != nil {
		t.Fatal(err)
	}
	return &protocol.Signed{Payload: p.Payload, Signatures: h.Signatures}
}

func TestSaveLoad(t *testing.T) {
	id, l := lake(t)
	dir := filepath.Join(t.TempDir(), "lakes", "work")
	if _, ok, err := Load(dir, l); ok || err != nil {
		t.Fatalf("empty: %v %v", ok, err)
	}
	d, err := Verify(sign(t, id, id.LakeID, `{"agent":{"debounce":"2s"}}`), l)
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	got, ok, err := Load(dir, l)
	if !ok || err != nil || got.Profile.Agent.Debounce != "2s" {
		t.Fatalf("%+v %v %v", got, ok, err)
	}
	// A new pin does not accept the old lake's cached copy.
	_, other := lake(t)
	if _, ok, err := Load(dir, other); ok || err == nil {
		t.Fatalf("cached copy passed another pin: %v %v", ok, err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, FileName))
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(strings.Replace(string(raw), "2s", "0s", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := Load(dir, l); ok || err == nil {
		t.Fatalf("edited cache passed: %v %v", ok, err)
	}
	if errors.Is(err, ErrNotPinned) {
		t.Fatal("wrong error")
	}
}
