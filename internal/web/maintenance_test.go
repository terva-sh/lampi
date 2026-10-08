package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/recall"
	"terva.sh/lampi/internal/registrar"
	"terva.sh/lampi/internal/webconfig"
)

func TestMaintenanceAdmissionFailureAndShutdown(t *testing.T) {
	started := make(chan struct{})
	m := NewMaintenance(t.Context(), func(ctx context.Context, action string) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	})
	record := func(context.Context, string, string) error { return nil }
	if err := m.start(t.Context(), "sample", func(context.Context, string, string) error { return errors.New("audit unavailable") }); err == nil || m.status().Running {
		t.Fatal("started without durable audit")
	}
	if err := m.start(t.Context(), "sample", record); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := m.start(t.Context(), "uploads", record); !errors.Is(err, errMaintenanceBusy) {
		t.Fatal("admitted concurrent job", err)
	}
	m.Close()
	if st := m.status(); st.Running || !strings.Contains(st.Message, "did not finish") {
		t.Fatal("shutdown status", st)
	}
	if err := m.start(t.Context(), "sample", record); !errors.Is(err, errMaintenanceBusy) {
		t.Fatal("admitted after close", err)
	}
}

func TestOperationsMaintenanceIsAdminCSRFAndFreshnessGuarded(t *testing.T) {
	lake, idp, _, dir := rawLake(t, nil)
	started := make(chan string, 1)
	release := make(chan struct{})
	m := NewMaintenance(t.Context(), func(ctx context.Context, action string) (string, error) {
		started <- action
		select {
		case <-release:
			return "Storage measurements refreshed.", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	t.Cleanup(m.Close)
	cfg := webconfig.Config{BaseURL: "https://lake.example", OIDC: webconfig.OIDC{Issuer: idp.URL(), ClientID: "lake", RoleMap: map[string]string{"readers": "viewer", "ops": "operator", "owners": "admin"}}}
	reg := &Registrations{Lake: func() registrar.Lake { return registrar.Lake{Catalog: lake.Catalog, Dir: dir} }}
	var err error
	lake.Web, err = New(cfg, lake.Catalog, recall.NewReader(lake.Catalog, lake.Normalized), nil, reg, &Operations{Maintenance: m}, idp.Client())
	if err != nil {
		t.Fatal(err)
	}
	h := lake.Handler()
	path := "/operations/maintenance"
	for _, group := range []string{"readers", "ops"} {
		cookie := signInAs(t, idp, h, group)
		if strings.Contains(get(h, "/operations", cookie).Body.String(), "Clean up old uploads") {
			t.Fatal("non-admin sees actions", group)
		}
		if w := postForm(h, path, url.Values{"action": {"sample"}, "csrf": {csrfOf(t, h, cookie)}}, cookie); w.Code != http.StatusNotFound {
			t.Fatal("non-admin action", group, w.Code)
		}
	}
	admin := signInAs(t, idp, h, "owners")
	csrf := csrfOf(t, h, admin)
	if !strings.Contains(get(h, "/operations", admin).Body.String(), "Clean up old uploads") {
		t.Fatal("admin missing controls")
	}
	if w := postForm(h, path, url.Values{"action": {"sample"}, "csrf": {"wrong"}}, admin); w.Code != http.StatusForbidden {
		t.Fatal("bad CSRF", w.Code)
	}
	for _, action := range []string{"purge", "search"} {
		if w := postForm(h, path, url.Values{"action": {action}, "csrf": {csrf}}, admin); w.Code != http.StatusBadRequest {
			t.Fatal("unsupported action", action, w.Code)
		}
	}
	// Expired freshness is refused before a job starts.
	idp.AuthTime = time.Now().Add(-time.Hour)
	old := signInAs(t, idp, h, "owners")
	if w := postForm(h, path, url.Values{"action": {"sample"}, "csrf": {csrfOf(t, h, old)}}, old); w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/auth/oidc/start") {
		t.Fatal("stale sign-in", w.Code)
	}
	w := postForm(h, path, url.Values{"action": {"sample"}, "csrf": {csrf}, "range": {"30d"}}, admin)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/operations?range=30d" {
		t.Fatal("request", w.Code, w.Body.String())
	}
	select {
	case action := <-started:
		if action != "sample" {
			t.Fatal(action)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("job did not start")
	}
	if w := postForm(h, path, url.Values{"action": {"uploads"}, "csrf": {csrf}}, admin); w.Code != http.StatusConflict {
		t.Fatal("parallel request", w.Code)
	}
	if body := get(h, "/operations", admin).Body.String(); !strings.Contains(body, `data-poll="true"`) || !strings.Contains(body, "Maintenance is running") {
		t.Fatal("running status")
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for m.status().Running && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if m.status().Running || !strings.Contains(get(h, "/operations", admin).Body.String(), "Storage measurements refreshed.") {
		t.Fatal("completion not shown")
	}
	b, err := os.ReadFile(filepath.Join(dir, audit.FileName))
	if err != nil || !strings.Contains(string(b), audit.MaintenanceRequested) || !strings.Contains(string(b), audit.MaintenanceFinished) {
		t.Fatal("missing audit records", err)
	}
}
