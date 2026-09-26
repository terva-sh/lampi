package webconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func valid() Config {
	return Config{BaseURL: "https://lake.example", OIDC: OIDC{Issuer: "https://id.example/issuer", ClientID: "lampi", RoleMap: map[string]string{"readers": "viewer"}}}
}
func TestValidation(t *testing.T) {
	for _, origin := range []string{"https://lake.example", "http://localhost:8787", "http://127.0.0.1:8787", "http://[::1]:8787"} {
		c := valid()
		c.BaseURL = origin
		if err := c.Validate(); err != nil {
			t.Fatal(err)
		}
		if c.CallbackURL() != origin+CallbackPath {
			t.Fatal(c.CallbackURL())
		}
	}
	for _, origin := range []string{"http://lake.example", "https://lake.example/", "https://lake.example/path", "https://user@lake.example", "https://lake.example?q=x", "https://lake.example#x", "//lake.example", "http://127.0.0.1.evil"} {
		c := valid()
		c.BaseURL = origin
		if c.Validate() == nil {
			t.Fatalf("accepted origin %q", origin)
		}
	}
	for _, edit := range []func(*Config){func(c *Config) { c.OIDC.Issuer = "http://id.example" }, func(c *Config) { c.OIDC.ClientID = " " }, func(c *Config) { c.OIDC.RoleMap = nil }, func(c *Config) { c.OIDC.RoleMap["readers"] = "owner" }} {
		c := valid()
		edit(&c)
		if c.Validate() == nil {
			t.Fatal("accepted bad config")
		}
	}
	c := valid()
	c.OIDC.Scopes = []string{"openid", "email", "email"}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(c.OIDC.Scopes, ",") != "openid,email" {
		t.Fatal(c.OIDC.Scopes)
	}
}
func TestLoadDoesNotEchoInputOrMarshalSecret(t *testing.T) {
	p := filepath.Join(t.TempDir(), "web.json")
	for _, s := range []string{`{"credential-marker":1}`, `{"base_url":`, `null`, `{}` + `{}`} {
		if err := os.WriteFile(p, []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := Load(p)
		if err == nil || strings.Contains(err.Error(), "credential-marker") {
			t.Fatalf("unsafe result %v", err)
		}
	}
	c := valid()
	c.OIDC.ClientSecretFile = filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(c.OIDC.ClientSecretFile, []byte("synthetic-test-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(c)
	if err := os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Secret() != "synthetic-test-secret" {
		t.Fatal("secret not loaded")
	}
	b, _ = json.Marshal(got)
	if strings.Contains(string(b), got.Secret()) {
		t.Fatal("secret serialized")
	}
}
