package web

import (
	"html"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/protocol"
)

// TKT-01M6F7JMWA46XCWJGV4VPZCFED: the mint form offers bays as
// checkboxes, and List matching sessions fills the session list from a
// project or machine pick without minting.
func TestReadTokenFormPickers(t *testing.T) {
	lake, idp, h, _ := rawLake(t, nil)
	ctx := t.Context()
	now := time.Now()
	var uids []string
	for _, n := range []string{"s-app", "s-lib"} {
		m := protocol.Manifest{CaptureProtocol: protocol.Version, MachineID: "machine-a", Harness: "claude", NativeSessionID: n,
			Project:   protocol.Project{GitRemote: "git@github.com:acme/" + strings.TrimPrefix(n, "s-") + ".git", CWD: "/w/" + n},
			Artifacts: []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: "p/" + n + ".jsonl", SHA256: putBlob(t, lake, []byte(n+"\n")), Size: int64(len(n) + 1)}}}
		ack, err := lake.Catalog.Ingest(ctx, m, now, []catalog.Decision{{Relation: protocol.RelationHead, Record: true, Head: true}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		uids = append(uids, ack.SessionUID)
	}
	if _, err := lake.Catalog.CreateBay(ctx, "research", "test", now); err != nil {
		t.Fatal(err)
	}
	admin := signInAs(t, idp, h, "owners")
	page := html.UnescapeString(get(h, adminReadTokensPath, admin).Body.String())
	for _, want := range []string{
		`<input type="checkbox" name="bay" value="research" >research</label>`,
		`<option value="github.com/acme" label="owner · 2 repositories · 2 sessions">`,
		`name="fill" value="1" formnovalidate data-enter-target="token-fill">List matching sessions</button>`,
		`<button class="default-submit" type="submit" tabindex="-1" aria-hidden="true">Mint token</button>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("mint form lacks %q", want)
		}
	}
	form := func(extra url.Values) url.Values {
		a := attemptField.FindStringSubmatch(get(h, adminReadTokensPath, admin).Body.String())
		v := url.Values{"csrf": {csrfOf(t, h, admin)}, "attempt": {a[1]}, "label": {"recall"}, "expires": {"24h"}, "permissions": {catalog.PermEventsRead}}
		for k, vs := range extra {
			v[k] = vs
		}
		return v
	}

	w := postForm(h, adminReadTokensPath, form(url.Values{"fill": {"1"}, "match": {"git@github.com:acme"}}), admin)
	body := w.Body.String()
	if w.Code != 200 || mintedToken.MatchString(body) || !strings.Contains(body, "Listed the 2 matching sessions, newest first.") {
		i := strings.Index(body, "token-sessions")
		t.Fatalf("fill: %d %s", w.Code, body[i:min(len(body), i+1500)])
	}
	for _, uid := range uids {
		if !strings.Contains(body, uid) {
			t.Errorf("the filled list lacks %s", uid)
		}
	}
	if toks, _ := lake.Catalog.ReadTokens(ctx); len(toks) != 0 {
		t.Errorf("filling minted %d tokens", len(toks))
	}
	if !strings.Contains(body, `name="match" list="token-project-choices" data-enter="token-fill" value="git@github.com:acme"`) {
		t.Error("the form does not keep the pick")
	}

	// No match leaves a list already typed as it was.
	w = postForm(h, adminReadTokensPath, form(url.Values{"fill": {"1"}, "match": {"/nowhere"}, "sessions": {uids[0]}}), admin)
	if body := w.Body.String(); w.Code != 200 || !strings.Contains(body, "The session list was left as it was.") || !strings.Contains(body, ">"+uids[0]+"</textarea>") {
		t.Errorf("no match: %d", w.Code)
	}
	if w := postForm(h, adminReadTokensPath, form(url.Values{"fill": {"1"}}), admin); w.Code != 400 {
		t.Errorf("fill with no pick: %d", w.Code)
	}

	// A bay the checkboxes do not show, named by the older field, rides
	// through a redisplay in a hidden field, so the next mint keeps it.
	w = postForm(h, adminReadTokensPath, form(url.Values{"fill": {"1"}, "match": {"github.com/acme"}, "bays": {"elsewhere"}}), admin)
	if body := w.Body.String(); w.Code != 200 || !strings.Contains(body, `<input type="hidden" name="bay" value="elsewhere">`) {
		t.Errorf("a bay with no checkbox was dropped: %d", w.Code)
	}

	// A ticked bay limits the minted token.
	w = postForm(h, adminReadTokensPath, form(url.Values{"bay": {"research"}, "sessions": {strings.Join(uids, "\n")}}), admin)
	if w.Code != 200 || !mintedToken.MatchString(w.Body.String()) {
		t.Fatalf("mint with a bay: %d", w.Code)
	}
	toks, _ := lake.Catalog.ReadTokens(ctx)
	if len(toks) != 1 || !toks[0].BayScoped || len(toks[0].Bays) != 1 || !slices.Equal(toks[0].Sessions, uids) {
		t.Errorf("minted %+v", toks)
	}
}
