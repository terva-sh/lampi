package web

import (
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
)

func TestActivityAPIThroughLakeMux(t *testing.T) {
	lake, idp, h, _ := fixture(t)
	seedSession(t, lake.Catalog, "first")
	seedSession(t, lake.Catalog, "second")
	// A repost of the same head is not another update.
	seedSession(t, lake.Catalog, "first")

	if w := get(h, "/api/web/v1/activity", nil); w.Code != 401 {
		t.Fatalf("unauthenticated activity: %d", w.Code)
	}
	cookie, _ := signIn(t, idp, h)
	w := get(h, "/api/web/v1/activity", cookie)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("activity: %d %s", w.Code, w.Body.String())
	}
	var a catalog.Activity
	if err := json.Unmarshal(w.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	if a.Bucket != catalog.BucketDay || len(a.Buckets) != 7 || a.CoverageSince == nil || a.Units["updates"] == "" {
		t.Fatalf("default activity: %+v", a)
	}
	today := a.Buckets[6]
	if today.Start != time.Now().UTC().Truncate(24*time.Hour).Format(time.RFC3339) || today.Updates == nil || *today.Updates != 2 || *today.NetLogicalBytes != 24 {
		t.Fatalf("today: %+v", today)
	}
	// The lake was created in this test, so every earlier day is unmeasured.
	if a.Buckets[0].Coverage != catalog.CoverageNone || a.Buckets[0].Updates != nil || a.Totals.Updates != 2 {
		t.Fatalf("earlier days: %+v totals %+v", a.Buckets[0], a.Totals)
	}

	q := url.Values{"bucket": {"hour"}, "harness": {"terva"}}
	w = get(h, "/api/web/v1/activity?"+q.Encode(), cookie)
	if err := json.Unmarshal(w.Body.Bytes(), &a); err != nil || w.Code != 200 {
		t.Fatalf("hourly terva: %d %s", w.Code, w.Body.String())
	}
	if len(a.Buckets) != 24 || a.Harness != "terva" || a.Totals.Updates != 0 {
		t.Fatalf("hourly terva: %+v", a)
	}
	// Empty form fields take the defaults.
	if w := get(h, "/api/web/v1/activity?bucket=&from=&until=&harness=", cookie); w.Code != 200 {
		t.Fatalf("empty form fields: %d %s", w.Code, w.Body.String())
	}

	for _, bad := range []string{
		"bucket=week",
		"harness=vim",
		"harness=codex&harness=terva",
		"limit=5",
		"from=yesterday",
		"from=2026-09-01",
		"bucket=day&from=2026-01-01T00:00:00Z&until=2026-06-01T00:00:00Z",
		"bucket=hour&from=2026-09-01T00:00:00Z&until=2026-09-20T00:00:00Z",
		"from=2026-09-10T00:00:00Z&until=2026-09-01T00:00:00Z",
	} {
		w := get(h, "/api/web/v1/activity?"+bad, cookie)
		if w.Code != 400 || !json.Valid(w.Body.Bytes()) {
			t.Fatalf("%s: %d %s", bad, w.Code, w.Body.String())
		}
	}
}
