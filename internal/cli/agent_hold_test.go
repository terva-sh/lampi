package cli

import (
	"testing"
	"time"

	"terva.sh/lampi/internal/adapter/cursorcli"
)

// armHold wakes the loop once a held session may be read, and a zero
// time stops a hold that has not fired.
func TestArmHoldWakesTheLoop(t *testing.T) {
	r := &lakeRunner{kick: make(chan struct{}, 1)}
	r.armHold(time.Now().Add(-2 * time.Second))
	select {
	case <-r.kick:
	case <-time.After(5 * time.Second):
		t.Fatal("hold did not wake the loop")
	}

	r.armHold(time.Now().Add(time.Hour))
	r.armHold(time.Time{})
	r.retryMu.Lock()
	stopped := r.hold == nil
	r.retryMu.Unlock()
	if !stopped {
		t.Fatal("zero time left a hold armed")
	}
	select {
	case <-r.kick:
		t.Fatal("stopped hold woke the loop")
	default:
	}
}

// The agent waits for a Cursor CLI session to pause before it exports
// it. A one-shot sync builds its options elsewhere and does not.
func TestAgentRunnerSettlesCursorCLI(t *testing.T) {
	r := newLakeRunner(Env{}, agentLake{})
	if r.lake.opt.Settle != cursorcli.SettleAfter {
		t.Fatalf("settle %v", r.lake.opt.Settle)
	}
}
