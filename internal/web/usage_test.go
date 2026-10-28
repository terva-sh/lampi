package web

import (
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/normalize"
)

func publishUsage(t *testing.T, lake *api.Server, uid string, input, output, cached int) {
	t.Helper()
	publishNormalized(t, lake, uid, []normalize.Event{{SchemaVersion: 1, EventID: "u", SessionID: "s", Harness: "codex", RecordedAt: "2026-09-26T10:00:00Z", IngestedAt: "2026-09-26T10:00:01Z",
		Actor: normalize.ActorHarness, EventType: normalize.EventUsage, Usage: normalize.Usage{Input: &input, Output: &output, CacheRead: &cached}, Redaction: normalize.Redaction{Status: "none"}}})
}

// The overview shows tokens split into input, output, cache read and
// cache write, by harness, and counts only the sessions the viewer
// reads (TKT-01M61NTE24T6CZN1P92AV7164F).
func TestOverviewShowsTokensTheViewerReads(t *testing.T) {
	lake, idp, h, _ := fixture(t)
	ctx := t.Context()
	open, _ := storeSession(t, lake, "native-open", []byte("{}\n"))
	secret, _ := storeSession(t, lake, "native-secret", []byte("{}\n{}\n"))
	if _, err := lake.Catalog.CreateBay(ctx, "secret", "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := lake.Catalog.AddToBay(ctx, catalog.Membership{SessionUID: secret, Bay: "secret", Actor: "test", Via: catalog.ViaCLI}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := lake.Catalog.RemoveFromBay(ctx, catalog.Membership{SessionUID: secret, Bay: catalog.DefaultBayName, Actor: "test", Via: catalog.ViaCLI}, time.Now()); err != nil {
		t.Fatal(err)
	}
	// Codex counts cached input inside input: 1,500 in with 500 cached
	// is 1,000 plain input.
	publishUsage(t, lake, open, 1500, 250, 500)
	publishUsage(t, lake, secret, 9_000_000, 9_000_000, 0)
	if err := indexes[lake].Pass(ctx); err != nil {
		t.Fatal(err)
	}
	cookie, _ := signIn(t, idp, h)
	w := get(h, "/", cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{`id="usage-heading">Tokens`, `title="1,750">1.75K`, `title="1,000">1.00K`, `title="500">500`, `title="not reported">—`} {
		if !strings.Contains(body, want) {
			t.Fatalf("overview lacks %q", want)
		}
	}
	if strings.Contains(body, "9,000") || strings.Contains(body, "9.00M") {
		t.Fatal("overview counts tokens from a bay the viewer does not read")
	}
}

func TestShortCount(t *testing.T) {
	for n, want := range map[int64]string{0: "0", 987: "987", 1000: "1.00K", 12_400: "12.4K", 3_210_000: "3.21M", 105_000_000: "105M", 1_050_000_000: "1.05B", -2500: "−2.50K"} {
		if got := shortCount(n); got != want {
			t.Errorf("shortCount(%d) = %q, want %q", n, got, want)
		}
	}
}
