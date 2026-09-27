package identity

import (
	"crypto/rand"
	"errors"
	"os"
	"sync"
	"testing"
	"time"
)

func TestConcurrentRotatesDoNotUndoACompromisedRetire(t *testing.T) {
	const rounds, rotates = 40, 6
	for round := 0; round < rounds; round++ {
		dir := t.TempDir()
		id, err := New(rand.Reader, t0)
		if err != nil {
			t.Fatal(err)
		}
		old := id.Keys[0].ID
		// A second active key, so the first can be retired.
		if _, err := id.Rotate(rand.Reader, t0, 14*24*time.Hour); err != nil {
			t.Fatal(err)
		}
		if err := create(dir, id); err != nil {
			t.Fatal(err)
		}

		now := t0.Add(time.Hour)
		var wg sync.WaitGroup
		errs := make(chan error, rotates+1)
		start := make(chan struct{})
		for i := 0; i < rotates; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				errs <- Update(dir, func(id *Identity) error {
					_, err := id.Rotate(rand.Reader, now, 14*24*time.Hour)
					return err
				})
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- Update(dir, func(id *Identity) error { return id.Retire(old, now, true) })
		}()
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}

		got, err := Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Keys) != 2+rotates {
			t.Fatalf("round %d: %d keys, want %d: a rotation was lost", round, len(got.Keys), 2+rotates)
		}
		for _, k := range got.Keys {
			if k.ID == old && (k.Status != StatusRetired || !k.Compromised) {
				t.Fatalf("round %d: key %s is %s, compromised %v: the retirement was lost", round, k.ID, k.Status, k.Compromised)
			}
		}
	}
}

func TestUpdateSavesNothingWhenTheEditFails(t *testing.T) {
	dir := t.TempDir()
	id, err := New(rand.Reader, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := create(dir, id); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	refused := errors.New("refused")
	err = Update(dir, func(id *Identity) error {
		id.Keys[0].Status = StatusRetired
		return refused
	})
	if !errors.Is(err, refused) {
		t.Fatalf("update: %v", err)
	}
	after, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("identity.json changed after a failed edit")
	}
}

func TestUpdateOnALakeWithNoIdentityIsNotExist(t *testing.T) {
	err := Update(t.TempDir(), func(*Identity) error { return nil })
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("update: %v", err)
	}
}
