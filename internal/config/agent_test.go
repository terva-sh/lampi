package config

import (
	"strings"
	"testing"
)

// TKT-01M3M7M0TH: sociable unless config.json says strict, and a
// misspelling is an error rather than the looser mode.
func TestInventoryMode(t *testing.T) {
	for in, want := range map[string]string{"": "sociable", "sociable": "sociable", "strict": "strict"} {
		if got, err := (File{Inventory: in}).InventoryMode(); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, in := range []string{"Strict", "stritc", "none"} {
		if _, err := (File{Inventory: in}).InventoryMode(); err == nil || !strings.Contains(err.Error(), "inventory") {
			t.Errorf("%q: %v", in, err)
		}
	}
}
