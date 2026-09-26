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

func sign(t *testing.T, id *identity.Identity, lakeID, cfg string) *protocol.Signed {
	t.Helper()
	s, err := id.Sign(identity.ContextAgentConfig, protocol.AgentConfigPayload{
		LakeID: lakeID, Profile: "default", Version: "v", IssuedAt: now, Config: json.RawMessage(cfg),
	}, now)
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
