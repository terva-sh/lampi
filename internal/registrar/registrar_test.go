package registrar_test

import (
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/registrar"
)

func testLake(t *testing.T) registrar.Lake {
	t.Helper()
	dir := t.TempDir()
	s, err := api.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if _, err := s.EnsureIdentity(dir); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	if err := s.Catalog.SetPublicURL(t.Context(), srv.URL); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Catalog.PutProfile(t.Context(), "ci", []byte(`{}`), "test", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	return registrar.Lake{Catalog: s.Catalog, Identity: s.Identity(), Dir: dir}
}

func TestMintAndRevokeRecordTheActor(t *testing.T) {
	l := testLake(t)
	ctx := t.Context()
	now := time.Now()
	op := registrar.Actor{Catalog: "web:sub-1", Audit: "web:sub-1 (Op One)", Minter: catalog.Minter{Admin: true}}

	if _, err := registrar.Mint(ctx, l, "box", "", nil, 31*24*time.Hour, op, now); !errors.Is(err, registrar.ErrLifetime) {
		t.Fatalf("long lifetime: %v", err)
	}
	if _, err := registrar.Mint(ctx, l, "box", "nope", nil, time.Hour, op, now); !errors.Is(err, registrar.ErrNoProfile) {
		t.Fatalf("unknown profile: %v", err)
	}
	m, err := registrar.Mint(ctx, l, "box", "ci", nil, time.Hour, op, now)
	if err != nil {
		t.Fatal(err)
	}
	if m.Code == "" || m.Registration.CreatedBy != "web:sub-1" || m.Registration.Profile != "ci" || !strings.HasPrefix(m.Fingerprint, "SHA256:") || m.LakeID != l.Identity.LakeID {
		t.Fatalf("minted %+v", m)
	}
	r, err := registrar.Revoke(ctx, l, m.Registration.ID, op, now)
	if err != nil || r.RevokedBy != "web:sub-1" {
		t.Fatalf("revoke: %+v %v", r, err)
	}
	if _, err := registrar.Revoke(ctx, l, m.Registration.ID, op, now); !errors.Is(err, catalog.ErrRegistrationRevoked) {
		t.Fatalf("second revoke: %v", err)
	}

	raw, err := os.ReadFile(audit.Path(l.Dir))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), m.Code) {
		t.Fatal("audit holds the code")
	}
	for _, kind := range []string{audit.RegistrationCreated, audit.RegistrationRevoked} {
		want := `"kind":"` + kind + `","device":"box","actor":"web:sub-1 (Op One)"`
		if strings.Count(string(raw), want) != 1 {
			t.Fatalf("audit lacks one %s by the operator:\n%s", kind, raw)
		}
	}
}

func TestListRecordsExpiries(t *testing.T) {
	l := testLake(t)
	ctx := t.Context()
	now := time.Now()
	if _, err := registrar.Mint(ctx, l, "old", "", nil, time.Minute, registrar.Actor{Catalog: catalog.ActorCLI, Audit: "serve register", Minter: catalog.Minter{Admin: true}}, now); err != nil {
		t.Fatal(err)
	}
	regs, err := registrar.List(ctx, l, "web:sub-1", now.Add(time.Hour))
	if err != nil || len(regs) != 1 || regs[0].State(now.Add(time.Hour)) != "expired" {
		t.Fatalf("list: %+v %v", regs, err)
	}
	raw, _ := os.ReadFile(audit.Path(l.Dir))
	if strings.Count(string(raw), audit.RegistrationExpired) != 1 {
		t.Fatalf("expiry lines:\n%s", raw)
	}
	if _, err := registrar.List(ctx, l, "web:sub-1", now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(audit.Path(l.Dir))
	if strings.Count(string(raw), audit.RegistrationExpired) != 1 {
		t.Fatalf("expiry written twice:\n%s", raw)
	}
}
