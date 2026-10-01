package catalog

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// TKT-01M3NM6FW7: a read token keeps its hash, its scope and its
// permissions; revoking twice is reported, not audited twice.
func TestReadTokens(t *testing.T) {
	c, _ := openTemp(t)
	ctx := t.Context()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	hash := strings.Repeat("e", 64)
	tok, err := c.CreateReadToken(ctx, ReadToken{Label: "scoped", Permissions: []string{PermRawRead}, Sessions: []string{"01A", "01B"}, CreatedBy: "web:admin", Expires: now.Add(time.Hour)}, hash, now)
	if err != nil || !strings.HasPrefix(tok.ID, "rtk_") {
		t.Fatalf("create %+v %v", tok, err)
	}
	got, ok, err := c.ReadTokenBySecret(ctx, hash)
	if err != nil || !ok || got.ID != tok.ID || strings.Join(got.Sessions, ",") != "01A,01B" || !got.Created.Equal(now) {
		t.Fatalf("lookup %+v %v %v", got, ok, err)
	}
	if _, ok, _ := c.ReadTokenBySecret(ctx, strings.Repeat("f", 64)); ok {
		t.Fatal("found an unknown secret")
	}
	for _, tc := range []struct {
		perm, uid string
		at        time.Time
		want      bool
	}{
		{PermRawRead, "01A", now, true},
		{PermRawRead, "01C", now, false},
		{"search:read", "01A", now, false},
		{PermRawRead, "01A", now.Add(time.Hour), false},
	} {
		if got.Allows(tc.perm, tc.uid, tc.at) != tc.want {
			t.Errorf("Allows(%s, %s, %v) != %v", tc.perm, tc.uid, tc.at, tc.want)
		}
	}
	lake := ReadToken{Permissions: []string{PermRawRead}, Expires: now.Add(time.Hour)}
	if !lake.Allows(PermRawRead, "anything", now) {
		t.Fatal("lake-wide token refused a session")
	}
	if _, err := c.CreateReadToken(ctx, ReadToken{Label: "dup", Permissions: []string{PermRawRead}, Expires: now.Add(time.Hour)}, hash, now); err == nil {
		t.Fatal("two tokens share a hash")
	}
	if err := c.TouchReadToken(ctx, tok.ID, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	r, err := c.RevokeReadToken(ctx, tok.ID, "web:admin", now.Add(2*time.Minute))
	if err != nil || r.State(now.Add(2*time.Minute)) != "revoked" {
		t.Fatalf("revoke %+v %v", r, err)
	}
	if _, err := c.RevokeReadToken(ctx, tok.ID, "web:admin", now); !errors.Is(err, ErrReadTokenRevoked) {
		t.Fatalf("second revoke: %v", err)
	}
	if _, err := c.RevokeReadToken(ctx, "rtk_nope", "web:admin", now); !errors.Is(err, ErrNoReadToken) {
		t.Fatalf("unknown revoke: %v", err)
	}
	all, err := c.ReadTokens(ctx)
	if err != nil || len(all) != 1 || all[0].LastUsed.IsZero() || all[0].RevokedBy != "web:admin" {
		t.Fatalf("list %+v %v", all, err)
	}
	if n, err := c.PendingAudit(ctx); err != nil || n != 2 {
		t.Fatalf("queued audit events %d %v, want created and revoked", n, err)
	}
}

// TKT-01M3FPWCH: a token holds raw:read, events:read or both, and each
// is checked on its own. An unknown or repeated permission is refused.
func TestReadTokenPermissions(t *testing.T) {
	c, _ := openTemp(t)
	ctx := t.Context()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for _, perms := range [][]string{nil, {"search:read"}, {PermRawRead, PermRawRead}} {
		if _, err := c.CreateReadToken(ctx, ReadToken{Label: "bad", Permissions: perms, Expires: now.Add(time.Hour)}, strings.Repeat("a", 64), now); err == nil {
			t.Errorf("minted a token with permissions %q", perms)
		}
	}
	both, err := c.CreateReadToken(ctx, ReadToken{Label: "agent", Permissions: []string{PermRawRead, PermEventsRead}, Expires: now.Add(time.Hour)}, strings.Repeat("b", 64), now)
	if err != nil {
		t.Fatal(err)
	}
	events := ReadToken{Permissions: []string{PermEventsRead}, Expires: now.Add(time.Hour)}
	if !both.Allows(PermEventsRead, "01A", now) || !both.Allows(PermRawRead, "01A", now) || events.Allows(PermRawRead, "01A", now) || !events.Allows(PermEventsRead, "01A", now) {
		t.Fatal("permissions are not checked one by one")
	}
}
