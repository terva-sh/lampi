package catalog

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// unscoped names every exported Catalog method that takes no Scope,
// with why it may (TKT-01M3NNF27A). A method that returns sessions, or
// counts or lists derived from them, to a reader whose bays are limited
// must take a Scope instead. TestEverySessionReadTakesAScope fails for a
// new method in neither place, so adding one is a decision, not an
// accident.
var unscoped = map[string]string{
	// Writes, lifecycle and configuration: they return no session data.
	"AddGrant": "write", "AddToBay": "write", "AliasBay": "write", "AllowTokens": "write",
	"BindMachine": "write", "Close": "lifecycle", "CreateBay": "write", "CreateReadToken": "write",
	"CreateRegistration": "write", "CreateRegistrationInBays": "write", "DeleteBay": "write",
	"DeleteNormalizeJob": "write", "DeleteProfile": "write", "DeleteProfileIf": "write",
	"DeleteSession": "write: serve purge on the lake host", "EnqueueNormalize": "write",
	"FlushAudit": "write", "HideProjects": "write", "Ingest": "write: returns only the poster's own session",
	"IngestChanged": "write: returns only the poster's own session", "MarkPublished": "write",
	"IngestRouted": "write: returns only the poster's own session", "AddBayRule": "write",
	"RemoveBayRule": "write", "ReleaseHold": "write", "BayRules": "rule listing for serve bays on the lake host",
	"Holds":    "serve bays holds on the lake host, which holds every bay",
	"Migrated": "lifecycle", "PutDeviceInventory": "write", "PutDeviceReport": "write",
	"PutProfile": "write", "PutProfileIf": "write", "PutProfilesIf": "write", "QueueAudit": "write",
	"RecordExpiries": "write", "RecordLakeID": "write", "RecordStorage": "write", "Redeem": "write",
	"RemoveFromBay": "write", "ResolveConflict": "write", "ReopenConflict": "write", "RemoveGrant": "write", "RenameBay": "write", "RevokeDevice": "write",
	"RevokeDeviceByID": "write", "RevokeReadToken": "write", "RevokeRegistration": "write",
	"SeedRoleGrants": "write", "SetDefaultEnabled": "write", "SetDeviceProfile": "write",
	"SetDeviceProfileByID": "write", "SetNormalizeError": "write", "SetPublicURL": "write",
	"SyncTokenFile": "write", "TouchReadToken": "write", "UnaliasBay": "write",
	"UnbindDevice": "write", "UnbindDeviceByID": "write", "UnhideProjects": "write",
	"VacuumInto": "serve backup on the lake host, which holds every bay",

	// Scope and access: they decide what a caller reads.
	"ScopeFor": "builds a scope", "DeviceScope": "builds a scope", "GroupBays": "builds a scope",
	"Grants": "grant listing for serve bays on the lake host", "ReadTokenReaches": "checks a read token's bays",
	"Bays": "bay names: the dashboard lists only those a user may use", "ResolveBay": "bay lookup",
	"BaySessionCounts": "serve bays list on the lake host",

	// Devices, profiles, registration, tokens, lake facts: not session data.
	"DeviceByHash": "device", "DeviceByID": "device", "DeviceByName": "device", "Devices": "device",
	"DeviceInventoryOf": "agent-reported inventory, not stored sessions", "DeviceReport": "device",
	"DeviceReports": "device", "HasProfile": "profile", "LatestProfileRevision": "profile",
	"ProfileByName": "profile", "ProfileNames": "profile", "ProfileRevisionByID": "profile",
	"ProfileRevisions": "profile", "Profiles": "profile", "RecentProfileRevisions": "profile",
	"ResolveProfile": "profile", "Registrations": "registration", "ReadTokenBySecret": "token",
	"ReadTokens": "token", "LakeID": "lake", "PublicURL": "lake", "SchemaVersion": "lake",
	"PendingAudit": "lake", "HiddenProjects": "review of agent inventories", "ReviewQueue": "review of agent inventories",
	"SightingsSince": "review of agent inventories", "HeadUpdatesSince": "a timestamp",
	"LatestStorage": "byte totals by component", "StorageSamples": "byte totals by component",
	"ArtifactBytes": "byte totals", "ReferencedDigests": "compact and fsck on the lake host",

	// One named session, for ingest, normalization, purge and export on
	// the lake host, or after the caller checked SessionInScope or read
	// DashboardSession with its scope.
	"Alias": "ingest", "Artifacts": "one session, after a scope check or on the lake host",
	"Current": "ingest", "Head": "ingest and export", "NormalizeError": "normalization",
	"NormalizeGen": "normalization", "NormalizeVersion": "normalization",
	"NormalizationState": "normalization", "OtherDigests": "purge", "Provenance": "one session, after a scope check",
	"Publication": "recall, after Reader.inScope", "Session": "one session, after a scope check or for an admin",
	"SessionBays": "one session's bays, after a scope check",

	// Whole-lake lists for workers, metrics and the lake host.
	"ListSessions": "export and normalize on the lake host", "SessionsByProject": "normalization",
	"PublishedSessions":   "the search indexer, which filters at query time",
	"NormalizationCounts": "the normalization backlog, a queue depth", "NormalizationStates": "serve normalize",
	"SessionsInNormalizationState": "serve normalize", "ListNormalizeJobs": "the normalize worker",
	"NormalizeBacklog": "the normalization backlog, a queue depth",
}

func TestEverySessionReadTakesAScope(t *testing.T) {
	fset := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var missing []string
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || !fn.Name.IsExported() || !receiverIs(fn, "Catalog") {
				continue
			}
			seen[fn.Name.Name] = true
			if takesScope(fn) {
				if _, listed := unscoped[fn.Name.Name]; listed {
					t.Errorf("%s takes a Scope and is also listed as unscoped", fn.Name.Name)
				}
				continue
			}
			if _, listed := unscoped[fn.Name.Name]; !listed {
				missing = append(missing, fn.Name.Name+" ("+fset.Position(fn.Pos()).String()+")")
			}
		}
	}
	sort.Strings(missing)
	for _, m := range missing {
		t.Errorf("Catalog.%s takes no Scope and is not in unscoped: give it a Scope if it returns session data, or list it with the reason it may not", m)
	}
	for name := range unscoped {
		if !seen[name] {
			t.Errorf("unscoped lists %s, which is not a Catalog method any more", name)
		}
	}
}

func receiverIs(fn *ast.FuncDecl, name string) bool {
	star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	id, ok := star.X.(*ast.Ident)
	return ok && id.Name == name
}

func takesScope(fn *ast.FuncDecl) bool {
	for _, p := range fn.Type.Params.List {
		if id, ok := p.Type.(*ast.Ident); ok && id.Name == "Scope" {
			return true
		}
	}
	return false
}

func TestScopeLimitsTheDashboard(t *testing.T) {
	ctx := context.Background()
	c, _ := openTemp(t)
	now := time.Now()
	inWork := newSession(t, c, "sess-work")
	inbox := newSession(t, c, "sess-inbox")
	work, _ := c.CreateBay(ctx, "work", "admin", now)
	if _, err := c.AddToBay(ctx, Membership{SessionUID: inWork, Bay: "work", Actor: "admin", Via: ViaCLI}, now); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveFromBay(ctx, Membership{SessionUID: inWork, Bay: DefaultBayName, Actor: "admin", Via: ViaCLI}, now); err != nil {
		t.Fatal(err)
	}
	scopes := map[string]Scope{"all": AllBays(), "work": InBays([]string{work.ID}), "none": {}, "empty": InBays(nil)}
	want := map[string][]string{"all": {inWork, inbox}, "work": {inWork}, "none": nil, "empty": nil}
	for name, scope := range scopes {
		page, err := c.DashboardSessions(ctx, scope, PageRequest{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, s := range page.Items {
			got = append(got, s.UID)
		}
		sort.Strings(got)
		w := append([]string(nil), want[name]...)
		sort.Strings(w)
		if strings.Join(got, ",") != strings.Join(w, ",") {
			t.Errorf("%s: sessions %v want %v", name, got, w)
		}
		ov, err := c.DashboardOverview(ctx, scope)
		if err != nil || ov.Sessions != int64(len(w)) {
			t.Errorf("%s: overview %d sessions err=%v", name, ov.Sessions, err)
		}
		n, err := c.Counts(ctx, scope)
		if err != nil || n.Sessions != len(w) {
			t.Errorf("%s: counts %d err=%v", name, n.Sessions, err)
		}
		_, err = c.DashboardSession(ctx, scope, inbox)
		if in := name == "all"; (err == nil) != in {
			t.Errorf("%s: inbox session visible=%v", name, err == nil)
		}
		ok, err := c.SessionInScope(ctx, scope, inWork)
		if err != nil || ok != (name == "all" || name == "work") {
			t.Errorf("%s: work session in scope=%v err=%v", name, ok, err)
		}
		recs, err := c.DashboardRecords(ctx, scope, inbox, "artifacts", PageRequest{Limit: 10})
		if err != nil || (len(recs.Items) > 0) != (name == "all") {
			t.Errorf("%s: inbox artifacts %d err=%v", name, len(recs.Items), err)
		}
	}
}
