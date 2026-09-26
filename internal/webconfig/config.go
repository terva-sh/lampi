// Package webconfig owns the explicit server-side browser configuration.
// It never reads the uploading agent's configuration or identity.
package webconfig

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const CallbackPath = "/auth/oidc/callback"

type Config struct {
	BaseURL string `json:"base_url"`
	OIDC    OIDC   `json:"oidc"`
	secret  string
}

type OIDC struct {
	Issuer           string            `json:"issuer"`
	ClientID         string            `json:"client_id"`
	ClientSecretFile string            `json:"client_secret_file,omitempty"`
	Scopes           []string          `json:"scopes,omitempty"`
	GroupsClaim      string            `json:"groups_claim,omitempty"`
	RoleMap          map[string]string `json:"role_map"`
}

// Load rejects malformed configuration without echoing its contents into an
// error. A misspelled field may itself contain a secret.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("web config: cannot open configuration file")
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > 64<<10 {
		return nil, errors.New("web config: configuration must be a regular file of at most 64 KiB")
	}
	d := json.NewDecoder(io.LimitReader(f, 64<<10))
	d.DisallowUnknownFields()
	var c Config
	if err := d.Decode(&c); err != nil {
		return nil, errors.New("web config: invalid JSON or unknown field")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, errors.New("web config: expected one JSON object")
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if c.OIDC.ClientSecretFile != "" {
		if !filepath.IsAbs(c.OIDC.ClientSecretFile) {
			return nil, errors.New("web config: client_secret_file must be absolute")
		}
		f, err := os.Open(c.OIDC.ClientSecretFile)
		if err != nil {
			return nil, errors.New("web config: cannot open client secret file")
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil || !st.Mode().IsRegular() || st.Size() > 4096 || (runtime.GOOS != "windows" && st.Mode().Perm()&0077 != 0) {
			return nil, errors.New("web config: client secret must be a private regular file of at most 4096 bytes")
		}
		b, err := io.ReadAll(io.LimitReader(f, 4097))
		if err != nil || len(b) > 4096 || strings.TrimSpace(string(b)) == "" {
			return nil, errors.New("web config: cannot read nonempty client secret")
		}
		c.secret = strings.TrimSpace(string(b))
	}
	return &c, nil
}

// Secret is used only when constructing the confidential OIDC client. It is
// private in the config representation and is never marshalled.
func (c *Config) Secret() string      { return c.secret }
func (c *Config) CallbackURL() string { return c.BaseURL + CallbackPath }
func (c *Config) Secure() bool        { return strings.HasPrefix(c.BaseURL, "https://") }

func (c *Config) Validate() error {
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || (u.Scheme != "https" && !(u.Scheme == "http" && loopback(u.Hostname()))) {
		return errors.New("web config: base_url must be an HTTPS origin (HTTP allowed only on loopback)")
	}
	if err := HTTPSURL(c.OIDC.Issuer); err != nil {
		return errors.New("web config: issuer must be an HTTPS URL")
	}
	if strings.TrimSpace(c.OIDC.ClientID) == "" {
		return errors.New("web config: client_id is required")
	}
	if len(c.OIDC.RoleMap) == 0 {
		return errors.New("web config: role_map must grant viewer to at least one group")
	}
	for group, role := range c.OIDC.RoleMap {
		if strings.TrimSpace(group) == "" || role != "viewer" {
			return errors.New("web config: role_map accepts nonempty groups mapped to viewer only")
		}
	}
	if c.OIDC.GroupsClaim == "" {
		c.OIDC.GroupsClaim = "groups"
	}
	if c.OIDC.Scopes == nil {
		c.OIDC.Scopes = []string{"profile", "email", "groups"}
	}
	scopes := []string{"openid"}
	seen := map[string]bool{"openid": true}
	for _, s := range c.OIDC.Scopes {
		if strings.TrimSpace(s) != s || s == "" || strings.ContainsAny(s, " \t\r\n") {
			return errors.New("web config: invalid scope")
		}
		if !seen[s] {
			scopes = append(scopes, s)
			seen[s] = true
		}
	}
	c.OIDC.Scopes = scopes
	return nil
}

// HTTPSURL validates provider endpoints without returning their contents.
func HTTPSURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return errors.New("OIDC endpoint must be HTTPS")
	}
	return nil
}

func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
