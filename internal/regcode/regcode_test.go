package regcode

import (
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/identity"
)

func TestEncodeDecode(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	id, err := identity.New(rand.Reader, now)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	code, err := Encode(id, Code{URL: "https://lake.example", Secret: secret, Expires: now.Add(24 * time.Hour)}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(code, Prefix) || strings.ContainsAny(code, "+/= \n") {
		t.Fatalf("code is not URL-safe: %s", code)
	}
	c, err := Decode(" " + code + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if c.URL != "https://lake.example" || c.LakeID != id.LakeID || c.Secret != secret || !c.Expires.Equal(now.Add(24*time.Hour)) {
		t.Fatalf("%+v", c)
	}
	if c.Fingerprint() != identity.Fingerprint(id.Keys[0].Pub) {
		t.Fatal("fingerprint does not match serve identity")
	}
	if len(HashSecret(secret)) != 64 || HashSecret(secret) == secret {
		t.Fatal("secret hash")
	}

	// Any change to the payload breaks the signature.
	parts := strings.Split(strings.TrimPrefix(code, Prefix), ".")
	payload, _ := b64.DecodeString(parts[0])
	forged := strings.Replace(string(payload), "lake.example", "evil.example", 1)
	if _, err := Decode(Prefix + b64.EncodeToString([]byte(forged)) + "." + parts[1]); err == nil || !strings.Contains(err.Error(), "does not verify") {
		t.Fatalf("forged URL: %v", err)
	}
	for in, want := range map[string]string{
		"hello":                   "not a registration code",
		"tlr2.a.b":                "newer terva-lampi",
		Prefix + "a":              "two parts",
		Prefix + "!!." + parts[1]: "payload does not decode",
	} {
		if _, err := Decode(in); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", in, err, want)
		}
	}
}
