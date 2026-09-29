package api

import (
	"net/http/httptest"
	"testing"
)

// A lake served without tokens reads every bay, as it answers every
// other request. A lake with tokens gives a request that carries no
// device nothing, rather than the whole lake.
func TestDeviceScopeWithoutADevice(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	r := httptest.NewRequest("GET", "/v1/stats", nil)
	if scope, err := s.deviceScope(r); err != nil || !scope.All() {
		t.Errorf("tokenless lake: all=%v %v", scope.All(), err)
	}
	s.Allow(laptopToken)
	if scope, err := s.deviceScope(r); err != nil || scope.All() || len(scope.Bays()) != 0 {
		t.Errorf("lake with tokens, no device: all=%v bays=%v %v", scope.All(), scope.Bays(), err)
	}
}

// A manifest or hello on a lake with tokens must carry a device: one
// without is refused, not taken as the tokenless lake that writes every
// bay.
func TestRequestDevice(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	r := httptest.NewRequest("POST", "/v1/manifests", nil)
	if id, ok := s.requestDevice(r); !ok || id != "" {
		t.Errorf("tokenless lake: %q %v", id, ok)
	}
	s.Allow(laptopToken)
	if id, ok := s.requestDevice(r); ok {
		t.Errorf("lake with tokens took a request with no device as %q", id)
	}
}
