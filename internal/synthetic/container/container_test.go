//go:build synthetic_container

package container

import "testing"

// TestSyntheticContainer keeps
// `go test -tags=synthetic_container ./internal/synthetic/container/`
// from failing with no packages. Gage replaces this skip with the
// container smoke driver.
func TestSyntheticContainer(t *testing.T) {
	t.Skip("synthetic container driver lands in a follow-up")
}
