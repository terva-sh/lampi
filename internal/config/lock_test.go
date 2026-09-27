package config

import (
	"fmt"
	"sync"
	"testing"
)

// TKT-01M3G8B8: writers that each read, edit and rename config.json do
// not lose one another's change. Each goroutine has its own descriptor
// on the lock file, as two processes would.
func TestConcurrentLakeWritesAreNotLost(t *testing.T) {
	dir := t.TempDir()
	env := envOf(map[string]string{"XDG_CONFIG_HOME": dir, "HOME": dir})
	const n = 24
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Go(func() {
			errs <- SetLake(env, fmt.Sprintf("lake-%02d", i), LakeConfig{Server: fmt.Sprintf("https://%02d.example", i)})
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	f, err := LoadFile(env)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Lakes) != n {
		t.Fatalf("%d of %d lakes survived", len(f.Lakes), n)
	}
}

func TestTxReadsTheEntryAndDoesNotLockAgain(t *testing.T) {
	dir := t.TempDir()
	env := envOf(map[string]string{"XDG_CONFIG_HOME": dir, "HOME": dir})
	err := Locked(env, func(tx Tx) error {
		if _, ok, err := tx.Lake("work"); err != nil || ok {
			return fmt.Errorf("missing lake: %v %v", ok, err)
		}
		if err := tx.SetLake("work", LakeConfig{Server: "https://work.example", DeviceID: "dev_1"}); err != nil {
			return err
		}
		if err := tx.UpdateLake("work", func(lc *LakeConfig) error { lc.Server = "https://moved.example"; return nil }); err != nil {
			return err
		}
		lc, ok, err := tx.Lake("work")
		if err != nil || !ok || lc.DeviceID != "dev_1" || lc.Server != "https://moved.example" {
			return fmt.Errorf("entry %+v %v %v", lc, ok, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
