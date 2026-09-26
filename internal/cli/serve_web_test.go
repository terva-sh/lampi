package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebRequiresDeviceTokensEvenOnLoopback(t *testing.T) {
	p := filepath.Join(t.TempDir(), "web.json")
	if err := os.WriteFile(p, []byte(`{"base_url":"https://lake.example","oidc":{"issuer":"https://id.example","client_id":"lake","role_map":{"readers":"viewer"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := runServe(Env{Stderr: &out}, []string{"--addr", "127.0.0.1:0", "--data", filepath.Join(t.TempDir(), "lake"), "--web-config", p})
	if err == nil || !strings.Contains(err.Error(), "nonempty device tokens") {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(out.String(), "listening") {
		t.Fatal("opened listener before validation")
	}
}
