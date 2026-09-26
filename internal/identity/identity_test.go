package identity

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/protocol"
)

var t0 = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

type memRecorder struct{ id string }

func (m *memRecorder) LakeID() (string, error) { return m.id, nil }
func (m *memRecorder) RecordLakeID(id string) error {
	if m.id != "" && m.id != id {
		return errors.New("already recorded")
	}
	m.id = id
	return nil
}

func TestEnsureMakesThenLoadsTheSameIdentity(t *testing.T) {
	dir := t.TempDir()
	rec := &memRecorder{}
	id, created, err := Ensure(dir, rec, rand.Reader, t0)
	if err != nil || !created {
		t.Fatalf("first Ensure created=%v err=%v", created, err)
	}
	if !ValidLakeID(id.LakeID) || rec.id != id.LakeID {
		t.Fatalf("lake id %q recorded %q", id.LakeID, rec.id)
	}
	st, err := os.Stat(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("identity.json mode %v", st.Mode().Perm())
	}
	again, created, err := Ensure(dir, rec, rand.Reader, t0.Add(time.Hour))
	if err != nil || created {
		t.Fatalf("second Ensure created=%v err=%v", created, err)
	}
	if again.LakeID != id.LakeID || again.Keys[0].ID != id.Keys[0].ID || !again.Keys[0].Pub.Equal(id.Keys[0].Pub) {
		t.Fatal("reload is a different identity")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temp file left behind: %v", entries)
	}
}

func TestEnsureRecordsAnExistingFileOnAnUnrecordedCatalog(t *testing.T) {
	dir := t.TempDir()
	id, _, err := Ensure(dir, &memRecorder{}, rand.Reader, t0)
	if err != nil {
		t.Fatal(err)
	}
	rec := &memRecorder{}
	if _, created, err := Ensure(dir, rec, rand.Reader, t0); err != nil || created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if rec.id != id.LakeID {
		t.Fatalf("recorded %q, want %q", rec.id, id.LakeID)
	}
}

func TestEnsureRefusesALostIdentity(t *testing.T) {
	dir := t.TempDir()
	rec := &memRecorder{}
	if _, _, err := Ensure(dir, rec, rand.Reader, t0); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(Path(dir)); err != nil {
		t.Fatal(err)
	}
	_, _, err := Ensure(dir, rec, rand.Reader, t0)
	if err == nil || !strings.Contains(err.Error(), "restore identity.json") {
		t.Fatalf("lost identity: %v", err)
	}
	if _, err := os.Stat(Path(dir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a new identity was made over a recorded lake id")
	}
}

func TestEnsureRefusesAnotherLakesIdentity(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := Ensure(dir, &memRecorder{}, rand.Reader, t0); err != nil {
		t.Fatal(err)
	}
	other := &memRecorder{id: "lake_" + strings.Repeat("a", 26)}
	if _, _, err := Ensure(dir, other, rand.Reader, t0); err == nil || !strings.Contains(err.Error(), "restore the matching identity.json") {
		t.Fatalf("mismatched identity: %v", err)
	}
}

func TestCreateDoesNotReplaceAnIdentity(t *testing.T) {
	dir := t.TempDir()
	a, _ := New(rand.Reader, t0)
	b, _ := New(rand.Reader, t0)
	if err := create(dir, a); err != nil {
		t.Fatal(err)
	}
	if err := create(dir, b); err == nil {
		t.Fatal("second create replaced the identity")
	}
	got, err := Load(dir)
	if err != nil || got.LakeID != a.LakeID {
		t.Fatalf("identity after a refused create: %v %v", got, err)
	}
}

func TestLoadRejectsAKeyThatDoesNotMatchItsID(t *testing.T) {
	dir := t.TempDir()
	id, _ := New(rand.Reader, t0)
	if err := create(dir, id); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(Path(dir))
	raw = []byte(strings.Replace(string(raw), id.Keys[0].ID, "0000000000000000", 1))
	if err := os.WriteFile(Path(dir), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "does not match its key") {
		t.Fatalf("tampered id: %v", err)
	}
}

func TestSignVerify(t *testing.T) {
	id, _ := New(rand.Reader, t0)
	s, err := id.Sign(ContextKeys, protocol.KeysPayload{LakeID: id.LakeID, Nonce: "abc", IssuedAt: t0, Keys: id.Public()}, t0)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := ParsePublic(id.Public()[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(ContextKeys, s, pub); err != nil {
		t.Fatal(err)
	}
	// The same bytes do not verify for another use.
	if err := Verify(ContextHello, s, pub); err == nil {
		t.Fatal("keys signature verified as a hello proof")
	}
	// A changed payload does not verify.
	bad := *s
	bad.Payload = json.RawMessage(strings.Replace(string(s.Payload), `"abc"`, `"abd"`, 1))
	if err := Verify(ContextKeys, &bad, pub); err == nil {
		t.Fatal("tampered payload verified")
	}
	// Another key does not verify.
	other, _ := New(rand.Reader, t0)
	if err := Verify(ContextKeys, s, other.Keys[0].Pub); err == nil {
		t.Fatal("another lake's key verified")
	}
	// The payload survives a JSON round trip byte for byte.
	raw, _ := json.Marshal(s)
	var back protocol.Signed
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if err := Verify(ContextKeys, &back, pub); err != nil {
		t.Fatalf("after a round trip: %v", err)
	}
}

func TestRetiredAndExpiredKeysDoNotSign(t *testing.T) {
	id, _ := New(rand.Reader, t0)
	id.Keys[0].NotAfter = t0.Add(time.Hour)
	if _, err := id.Sign(ContextHello, 1, t0.Add(2*time.Hour)); err == nil {
		t.Fatal("expired key signed")
	}
	id.Keys[0].NotAfter = time.Time{}
	id.Keys[0].Status = StatusRetired
	if _, err := id.Sign(ContextHello, 1, t0); err == nil {
		t.Fatal("retired key signed")
	}
}

func TestParsePublicChecksTheID(t *testing.T) {
	id, _ := New(rand.Reader, t0)
	k := id.Public()[0]
	k.ID = "0000000000000000"
	if _, err := ParsePublic(k); err == nil {
		t.Fatal("key with a wrong id parsed")
	}
}

func TestValidNonce(t *testing.T) {
	for _, ok := range []string{"", "abc", "A-_9", strings.Repeat("a", MaxNonce)} {
		if !ValidNonce(ok) {
			t.Errorf("%q refused", ok)
		}
	}
	for _, bad := range []string{"a b", "a/b", "a=", strings.Repeat("a", MaxNonce+1), "é"} {
		if ValidNonce(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
	n, err := NewNonce()
	if err != nil || !ValidNonce(n) || len(n) != 43 {
		t.Fatalf("NewNonce %q %v", n, err)
	}
}

func TestFingerprintShape(t *testing.T) {
	id, _ := New(rand.Reader, t0)
	fp := Fingerprint(id.Keys[0].Pub)
	if !strings.HasPrefix(fp, "SHA256:") || len(fp) != len("SHA256:")+43 {
		t.Fatalf("fingerprint %q", fp)
	}
	if filepath.Base(Path("/x")) != FileName {
		t.Fatal("path")
	}
}

func TestLoadRejectsTrailingData(t *testing.T) {
	for _, tail := range []string{"{}", "x", "\n{\"version\":1}"} {
		dir := t.TempDir()
		id, _ := New(rand.Reader, t0)
		if err := create(dir, id); err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(Path(dir))
		if err := os.WriteFile(Path(dir), append(raw, tail...), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(dir); err == nil {
			t.Fatalf("trailing %q accepted", tail)
		}
	}
}
