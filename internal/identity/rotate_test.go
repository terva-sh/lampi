package identity

import (
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"terva.sh/lampi/internal/protocol"
)

func TestRotateChainsAndRetireCompromisedBreaksIt(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	id, err := New(rand.Reader, now)
	if err != nil {
		t.Fatal(err)
	}
	pin := id.Public()[0]
	if err := id.Retire(pin.ID, now, false); err == nil {
		t.Fatal("retired the only active key")
	}
	k2, err := id.Rotate(rand.Reader, now.Add(time.Hour), 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if k2.EndorsedBy != pin.ID {
		t.Fatalf("endorsed by %s", k2.EndorsedBy)
	}
	if cur, _ := id.Current(now.Add(time.Hour)); cur.ID != k2.ID {
		t.Fatal("current is not the new key")
	}
	// During the overlap both keys are active and the pin moves.
	at := now.Add(2 * time.Hour)
	if len(id.ActiveKeys(at)) != 2 {
		t.Fatalf("active %d", len(id.ActiveKeys(at)))
	}
	got, err := Advance(id.LakeID, pin, id.Public(), at)
	if err != nil || got.ID != k2.ID {
		t.Fatalf("advance: %+v %v", got, err)
	}
	// After the overlap the old key is not active; the chain still holds.
	late := now.Add(9 * 24 * time.Hour)
	if got, err := Advance(id.LakeID, pin, id.Public(), late); err != nil || got.ID != k2.ID {
		t.Fatalf("after overlap: %+v %v", got, err)
	}
	// Save and load keep the chain.
	dir := t.TempDir()
	if err := create(dir, id); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Advance(loaded.LakeID, pin, loaded.Public(), at); err != nil || got.ID != k2.ID {
		t.Fatalf("after load: %+v %v", got, err)
	}
	// A pin from another lake has no chain.
	other, _ := New(rand.Reader, now)
	if _, err := Advance(id.LakeID, other.Public()[0], id.Public(), at); !errors.Is(err, ErrNoChain) {
		t.Fatalf("stranger: %v", err)
	}
	// A forged endorsement is not followed.
	forged := id.Public()
	forged[1].Endorsement = forged[0].PublicKey
	if _, err := Advance(id.LakeID, pin, forged[1:], late); !errors.Is(err, ErrNoChain) {
		t.Fatalf("forged endorsement: %v", err)
	}

	// Compromise: agents pinned to the old key stop; those on the new
	// key keep going.
	if err := loaded.Retire(pin.ID, at, true); err != nil {
		t.Fatal(err)
	}
	if err := save(dir, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Advance(loaded.LakeID, pin, loaded.Public(), at); !errors.Is(err, ErrPinCompromised) {
		t.Fatalf("compromised pin: %v", err)
	}
	var newPin protocol.LakeKey
	for _, k := range loaded.Public() {
		if k.ID == k2.ID {
			newPin = k
		}
	}
	if got, err := Advance(loaded.LakeID, newPin, loaded.Public(), at); err != nil || got.ID != k2.ID {
		t.Fatalf("new pin: %+v %v", got, err)
	}
	if loaded.ActiveKeys(at)[0].ID != k2.ID || len(loaded.ActiveKeys(at)) != 1 {
		t.Fatal("compromised key still signs")
	}
}
