package cli

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/auth"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/web"
	"terva.sh/lampi/internal/webconfig"
)

func TestServeBays(t *testing.T) {
	dir := t.TempDir()
	tokens := t.TempDir()
	if err := os.WriteFile(filepath.Join(tokens, "laptop.token"), []byte(strings.Repeat("c3", 32)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	devices, err := auth.LoadDevices(tokens)
	if err != nil {
		t.Fatal(err)
	}
	lake, err := api.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	lake.Devices = devices
	if err := lake.SyncDevices(t.Context()); err != nil {
		t.Fatal(err)
	}
	lake.Close()
	run := func(args ...string) (string, error) {
		var out bytes.Buffer
		err := Run(append([]string{"serve", "bays"}, append(args, "--data", dir)...), Env{Stdout: &out, Stderr: ioDiscard()})
		return out.String(), err
	}
	must := func(want string, args ...string) {
		t.Helper()
		out, err := run(args...)
		if err != nil || !strings.Contains(out, want) {
			t.Fatalf("%v: %q %v", args, out, err)
		}
	}
	must("default id=bay_default sessions=0 default", "list")
	must("default device laptop write by=serve", "grants")
	must("created bay client-x", "create", "client-x")
	if _, err := run("create", "Client X"); err == nil {
		t.Fatal("bad name accepted")
	}
	must("renamed client-x to acme", "rename", "client-x", "acme")
	must("acme id=bay_", "list")
	must("aliases=client-x", "list")
	// Renamed through an alias, the bay keeps its own name as an alias.
	must("created bay zeta", "create", "zeta")
	must("z is now an alias of zeta", "alias", "zeta", "z")
	must("renamed zeta to omega; zeta stays as an alias", "rename", "z", "omega")
	// And back: a bay takes its own former name from its aliases.
	must("renamed omega to zeta; omega stays as an alias", "rename", "omega", "zeta")
	must("zeta id=bay_", "list")
	if _, err := run("rename", "zeta", "client-x"); err == nil {
		t.Fatal("a bay took another bay's alias as its name")
	}
	must("inbox is now an alias of default", "alias", "default", "inbox")
	if _, err := run("rename", "default", "other"); err == nil {
		t.Fatal("renamed the default bay")
	}
	must("granted group eng read on acme", "grant", "acme", "--group", "eng", "--read")
	must("already holds", "grant", "client-x", "--group", "eng", "--read")
	must("granted device", "grant", "acme", "--device", "laptop", "--write")
	if _, err := run("grant", "acme", "--device", "laptop", "--read"); err == nil {
		t.Fatal("device read accepted")
	}
	if _, err := run("grant", "acme", "--group", "eng"); err == nil {
		t.Fatal("grant without a permission accepted")
	}
	if _, err := run("grant", "acme", "--group", "eng", "--device", "laptop", "--read"); err == nil {
		t.Fatal("two principals accepted")
	}
	must("acme device laptop write", "grants")
	must("revoked group eng read on acme", "revoke", "acme", "--group", "eng", "--read")
	must("default bay turned off", "default", "off")
	must("default off", "list")
	if _, err := run("delete", "acme"); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("delete without --yes: %v", err)
	}
	must("deleted bay acme; 0 sessions", "delete", "acme", "--yes")
	if _, err := run("delete", "default", "--yes"); err == nil {
		t.Fatal("deleted the default bay")
	}
	raw, _ := os.ReadFile(audit.Path(dir))
	for _, kind := range []string{audit.BayCreated, audit.BayRenamed, audit.BayAliasAdded, audit.BayGrantAdded, audit.BayGrantRemoved, audit.BayDefault, audit.BayDeleted} {
		if !strings.Contains(string(raw), `"kind":"`+kind+`"`) {
			t.Fatalf("audit has no %s:\n%s", kind, raw)
		}
	}
}

func TestSeedBayGrantsOnUpgrade(t *testing.T) {
	dir := t.TempDir()
	cat, err := catalog.Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cat.Close()
	cfg := webconfig.Config{OIDC: webconfig.OIDC{RoleMap: map[string]string{"readers": "viewer", "ops": "operator", "admins": "admin"}}}
	var logged bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logged, nil))
	if err := web.SeedBayGrants(log, cat, cfg); err != nil {
		t.Fatal(err)
	}
	grants, _ := cat.Grants(t.Context(), "", "")
	var got []string
	for _, g := range grants {
		got = append(got, g.Principal+":"+g.Permission)
	}
	if want := []string{"ops:read", "ops:write", "readers:read"}; !slices.Equal(got, want) {
		t.Fatalf("grants %v want %v", got, want)
	}
	// A group added later gets nothing and is named at startup.
	cfg.OIDC.RoleMap["late"] = "viewer"
	logged.Reset()
	if err := web.SeedBayGrants(log, cat, cfg); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logged.String(), "group=late") || strings.Contains(logged.String(), "group=readers") {
		t.Fatalf("startup log:\n%s", logged.String())
	}
	if err := cat.FlushAudit(t.Context(), dir); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(audit.Path(dir))
	if strings.Count(string(raw), `"kind":"bay.grant.added"`) != 3 {
		t.Fatalf("audit:\n%s", raw)
	}
}

// TestSeedGrantLinesReachTheAuditOnALaterStart is review 1403: a start
// that cannot write audit.jsonl leaves the grant lines queued, and the
// next start writes them.
func TestSeedGrantLinesReachTheAuditOnALaterStart(t *testing.T) {
	dir := t.TempDir()
	cat, err := catalog.Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cat.Close()
	cfg := webconfig.Config{OIDC: webconfig.OIDC{RoleMap: map[string]string{"readers": "viewer"}}}
	// A directory where the log goes makes the write fail.
	if err := os.Mkdir(audit.Path(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	// What startWeb does on every start: seed through web.New, then
	// flush whatever is queued.
	start := func() error {
		if err := web.SeedBayGrants(nil, cat, cfg); err != nil {
			t.Fatal(err)
		}
		return cat.FlushAudit(t.Context(), dir)
	}
	if err := start(); err == nil {
		t.Fatal("the audit write did not fail")
	}
	if err := os.Remove(audit.Path(dir)); err != nil {
		t.Fatal(err)
	}
	if err := start(); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(audit.Path(dir))
	if strings.Count(string(raw), `"kind":"bay.grant.added"`) != 1 {
		t.Fatalf("audit after the second start:\n%s", raw)
	}
}

func TestServeBaysRules(t *testing.T) {
	dir := t.TempDir()
	lake, err := api.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	lake.Close()
	run := func(args ...string) (string, error) {
		var out bytes.Buffer
		err := Run(append([]string{"serve", "bays"}, append(args, "--data", dir)...), Env{Stdout: &out, Stderr: ioDiscard()})
		return out.String(), err
	}
	must := func(want string, args ...string) {
		t.Helper()
		out, err := run(args...)
		if err != nil || !strings.Contains(out, want) {
			t.Fatalf("%v: %q %v", args, out, err)
		}
	}
	must("no rules", "rules")
	must("created bay review", "create", "review")
	must("added rule 1: hold review when cwd-prefix=/src/client harness=claude", "rule", "review", "--hold", "--cwd-prefix", "/src/client", "--harness", "claude")
	must("added rule 2: add default when git-remote-prefix=git@host:org", "rule", "default", "--add", "--git-remote-prefix", "git@host:org")
	for name, args := range map[string][]string{
		"no action":     {"rule", "review", "--cwd-prefix", "/x"},
		"two actions":   {"rule", "review", "--add", "--deny", "--cwd-prefix", "/x"},
		"no match":      {"rule", "review", "--add"},
		"unknown bay":   {"rule", "nope", "--add", "--cwd-prefix", "/x"},
		"bad glob":      {"rule", "review", "--add", "--cwd-glob", "src/*"},
		"bad rule id":   {"unrule", "one"},
		"missing rule":  {"unrule", "9"},
		"not held":      {"release", "01ARZ3NDEKTSV4RRFFQ69G5FAV"},
		"no release id": {"release"},
	} {
		if _, err := run(args...); err == nil {
			t.Errorf("%s: %v accepted", name, args)
		}
	}
	must("1 hold review when cwd-prefix=/src/client harness=claude by=serve bays rule", "rules")
	must("removed rule 1", "unrule", "1")
	out, err := run("rules")
	if err != nil || strings.Contains(out, "hold review") || !strings.Contains(out, "2 add default") {
		t.Fatalf("rules after unrule: %q %v", out, err)
	}
	must("no holds", "holds")
	raw, err := os.ReadFile(filepath.Join(dir, audit.FileName))
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{audit.BayRuleAdded, audit.BayRuleRemoved} {
		if !strings.Contains(string(raw), `"`+kind+`"`) {
			t.Errorf("audit log has no %s", kind)
		}
	}
}
