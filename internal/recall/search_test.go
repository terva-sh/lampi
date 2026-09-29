package recall

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/protocol"
)

func ingestAs(t *testing.T, s *api.Server, harness, native, remote string) string {
	t.Helper()
	m := protocol.Manifest{CaptureProtocol: protocol.Version, MachineID: "machine-a", Harness: harness, NativeSessionID: native,
		Project:   protocol.Project{GitRemote: remote, GitRoot: root(remote)},
		Artifacts: []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: "s.jsonl", SHA256: strings.Repeat("c", 64), Size: 12}}}
	ack, err := s.Catalog.Ingest(t.Context(), m, time.Now(), []catalog.Decision{{Relation: protocol.RelationHead, Record: true, Head: true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ack.SessionUID
}

// root is a synthetic root commit, empty when there is no remote.
func root(remote string) string {
	if remote == "" {
		return ""
	}
	return strings.Repeat("d", 40)
}

func openIndex(t *testing.T, s *api.Server) *Index {
	t.Helper()
	x, err := OpenIndex(filepath.Join(t.TempDir(), IndexFile), NewReader(s.Catalog, s.Normalized))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { x.Close() })
	return x
}

func pass(t *testing.T, x *Index) {
	t.Helper()
	if err := x.Pass(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func search(t *testing.T, x *Index, req SearchRequest) SearchPage {
	t.Helper()
	p, err := x.Search(t.Context(), req)
	if err != nil {
		t.Fatal(req.Query, err)
	}
	return p
}

func TestSearchIsLiteralAndCaseInsensitive(t *testing.T) {
	s := lake(t)
	uid := ingest(t, s, "literal")
	texts := []string{
		"Then I ran GIT PUSH --force to origin",
		`she said "hello" OR goodbye NEAR(x) col:value * AND`,
		"Ünïcode Straße",
		"nothing to see",
	}
	publish(t, s, uid, events(len(texts), func(i int) string { return texts[i] }))
	x := openIndex(t, s)
	pass(t, x)
	for q, want := range map[string]int64{
		"git push --force": 0, "push --for": 0, `"hello" OR`: 1, "NEAR(x)": 1, "col:value": 1, "* AND": 1, "ünïCODE": 2, "straße": 2,
	} {
		p := search(t, x, SearchRequest{Query: q})
		if len(p.Items) != 1 || p.Items[0].Position != want {
			t.Fatalf("%q: %+v", q, p.Items)
		}
		h := p.Items[0]
		if h.MatchLen == 0 || !strings.EqualFold(h.Snippet[h.MatchStart:h.MatchStart+h.MatchLen], q) {
			t.Fatalf("%q snippet %q [%d,%d)", q, h.Snippet, h.MatchStart, h.MatchLen)
		}
		if h.Link != EventLink(uid, h.Generation, h.Position) {
			t.Fatal("link", h.Link)
		}
	}
	for _, q := range []string{"hello OR goodbye", "git AND push", "gi*"} {
		if p := search(t, x, SearchRequest{Query: q}); len(p.Items) != 0 {
			t.Fatalf("%q parsed as syntax: %+v", q, p.Items)
		}
	}
	for _, q := range []string{"", "ab", "  ab  ", strings.Repeat("x", QueryMaxBytes+1), "abc\x00", "\xff\xfe\xfd"} {
		if _, err := x.Search(t.Context(), SearchRequest{Query: q}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("query %q accepted: %v", q, err)
		}
	}
	for _, req := range []SearchRequest{{Query: "abc", Limit: 201}, {Query: "abc", Limit: -1}, {Query: "abc", Harness: "vim"}, {Query: "abc", Project: "p", Unlinked: true}} {
		if _, err := x.Search(t.Context(), req); !errors.Is(err, ErrInvalid) {
			t.Fatalf("request %+v accepted: %v", req, err)
		}
	}
}

func TestSearchShowsOnlyCurrentGenerations(t *testing.T) {
	s := lake(t)
	uid := ingest(t, s, "generations")
	other := ingest(t, s, "other")
	publish(t, s, uid, events(3, func(int) string { return "alpha-old text" }))
	publish(t, s, other, events(1, func(int) string { return "alpha-old in another session" }))
	x := openIndex(t, s)
	pass(t, x)
	if p := search(t, x, SearchRequest{Query: "alpha-old"}); len(p.Items) != 4 {
		t.Fatal("before", len(p.Items))
	}
	// A new generation hides the old rows before the index catches up.
	publish(t, s, uid, events(2, func(int) string { return "beta-new text" }))
	p := search(t, x, SearchRequest{Query: "alpha-old"})
	if len(p.Items) != 1 || p.Items[0].SessionUID != other {
		t.Fatal("stale generation searchable", p.Items)
	}
	if cov := x.Coverage(); cov.Ready != 2 || cov.Indexed != 2 {
		t.Fatal("coverage before pass", cov)
	}
	pass(t, x)
	if cov := x.Coverage(); cov.Ready != 2 || cov.Indexed != 2 || cov.Behind != 0 {
		t.Fatal("coverage after pass", cov)
	}
	if p := search(t, x, SearchRequest{Query: "beta-new"}); len(p.Items) != 2 {
		t.Fatal("new generation", len(p.Items))
	}
	var rows int
	if err := x.db.QueryRow(`SELECT COUNT(*) FROM docs WHERE session_uid=?`, uid).Scan(&rows); err != nil || rows != 2 {
		t.Fatal("old rows kept", rows, err)
	}
	// Pending: hidden at once, and its rows are kept for the next generation.
	if _, err := s.Catalog.EnqueueNormalize(t.Context(), uid, time.Now()); err != nil {
		t.Fatal(err)
	}
	if p := search(t, x, SearchRequest{Query: "beta-new"}); len(p.Items) != 0 {
		t.Fatal("pending session searchable")
	}
	// Failed: hidden, then removed by a pass. The worker clears the job
	// row after recording the failure.
	gen, _, _, _ := s.Catalog.NormalizeVersion(t.Context(), uid)
	if err := s.StoreEvents(t.Context(), uid, nil, errors.New("synthetic")); err != nil {
		t.Fatal(err)
	}
	if err := s.Catalog.DeleteNormalizeJob(t.Context(), uid, gen); err != nil {
		t.Fatal(err)
	}
	pass(t, x)
	if err := x.db.QueryRow(`SELECT COUNT(*) FROM docs WHERE session_uid=?`, uid).Scan(&rows); err != nil || rows != 0 {
		t.Fatal("failed session rows kept", rows, err)
	}
	// Purge removes rows directly, and a pass cleans up a vanished session.
	path := filepath.Join(t.TempDir(), IndexFile)
	y, err := OpenIndex(path, NewReader(s.Catalog, s.Normalized))
	if err != nil {
		t.Fatal(err)
	}
	pass(t, y)
	y.Close()
	if err := RemoveFromIndex(t.Context(), path, other); err != nil {
		t.Fatal(err)
	}
	y, _ = OpenIndex(path, NewReader(s.Catalog, s.Normalized))
	defer y.Close()
	if p := search(t, y, SearchRequest{Query: "alpha-old"}); len(p.Items) != 0 {
		t.Fatal("purged session searchable")
	}
	if err := RemoveFromIndex(t.Context(), filepath.Join(t.TempDir(), "absent.db"), other); err != nil {
		t.Fatal("no index to purge", err)
	}
	plan, _, _ := s.PlanPurge(t.Context(), other)
	if err := s.Purge(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	pass(t, x)
	if err := x.db.QueryRow(`SELECT COUNT(*) FROM indexed`).Scan(&rows); err != nil || rows != 0 {
		t.Fatal("vanished session still indexed", rows, err)
	}
}

func TestIndexRecoversFromPartialWritesAndBadFiles(t *testing.T) {
	s := lake(t)
	uid := ingest(t, s, "partial")
	publish(t, s, uid, events(5, func(i int) string { return fmt.Sprint("recover me ", i) }))
	x := openIndex(t, s)
	// An attempt stopped part way writes nothing: a session is written
	// in one transaction.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := x.Pass(ctx); err == nil {
		t.Fatal("a stopped pass reported no error")
	}
	if p := search(t, x, SearchRequest{Query: "recover me"}); len(p.Items) != 0 {
		t.Fatal("rows of a stopped pass visible")
	}
	pass(t, x)
	if p := search(t, x, SearchRequest{Query: "recover me"}); len(p.Items) != 5 {
		t.Fatal("after recovery", len(p.Items))
	}
	// A ready session whose file is gone is reported and not retried.
	broken := ingest(t, s, "broken")
	publish(t, s, broken, events(1, func(int) string { return "x" }))
	if err := removeFile(s, broken); err != nil {
		t.Fatal(err)
	}
	pass(t, x)
	if cov := x.Coverage(); cov.Failed != 1 || cov.Indexed != 1 || cov.Ready != 2 {
		t.Fatal("failed coverage", cov)
	}
	pass(t, x)
	if cov := x.Coverage(); cov.Failed != 1 || cov.Behind != 0 {
		t.Fatal("failure retried", cov)
	}
	publish(t, s, broken, events(1, func(int) string { return "fixed now" }))
	pass(t, x)
	if cov := x.Coverage(); cov.Failed != 0 || cov.Indexed != 2 {
		t.Fatal("new generation after failure", cov)
	}
}

func TestIndexRebuildsUnknownVersions(t *testing.T) {
	path := filepath.Join(t.TempDir(), IndexFile)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE junk(x); PRAGMA user_version=99`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s := lake(t)
	x, err := OpenIndex(path, NewReader(s.Catalog, s.Normalized))
	if err != nil {
		t.Fatal(err)
	}
	defer x.Close()
	var v int
	if err := x.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil || v != indexVersion {
		t.Fatal("version", v, err)
	}
}

func TestSearchFiltersAndPaging(t *testing.T) {
	s := lake(t)
	a := ingestAs(t, s, "codex", "a", "git@example.com:org/one.git")
	b := ingestAs(t, s, "claude", "b", "")
	stamp := func(evs []normalize.Event, at func(i int) string) []normalize.Event {
		for i := range evs {
			evs[i].RecordedAt = at(i)
		}
		return evs
	}
	publish(t, s, a, stamp(events(120, func(i int) string { return fmt.Sprint("needle a ", i) }), func(i int) string {
		return time.Date(2026, 9, 1, 0, 0, i, 0, time.UTC).Format(time.RFC3339)
	}))
	publish(t, s, b, stamp(events(30, func(i int) string { return fmt.Sprint("needle b ", i) }), func(i int) string {
		if i%2 == 0 {
			return ""
		}
		return time.Date(2026, 9, 2, 0, 0, i, 0, time.UTC).Format(time.RFC3339)
	}))
	x := openIndex(t, s)
	pass(t, x)
	project := protocol.ProjectLinkID("git@example.com:org/one.git", root("x"))
	count := func(req SearchRequest) int {
		n := 0
		seen := map[string]bool{}
		for {
			p := search(t, x, req)
			for _, h := range p.Items {
				key := fmt.Sprint(h.SessionUID, h.Position)
				if seen[key] {
					t.Fatal("duplicate hit across pages", key)
				}
				seen[key] = true
				n++
			}
			if p.NextCursor == "" {
				return n
			}
			req.Cursor = p.NextCursor
		}
	}
	since := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 9, 1, 0, 1, 0, 0, time.UTC)
	for name, c := range map[string]struct {
		req  SearchRequest
		want int
	}{
		"all":        {SearchRequest{Query: "needle", Limit: 7}, 150},
		"harness":    {SearchRequest{Query: "needle", Harness: "claude"}, 30},
		"project":    {SearchRequest{Query: "needle", Project: project}, 120},
		"unlinked":   {SearchRequest{Query: "needle", Unlinked: true}, 30},
		"since":      {SearchRequest{Query: "needle", Since: &since}, 15},
		"until":      {SearchRequest{Query: "needle", Until: &until}, 60},
		"no project": {SearchRequest{Query: "needle", Project: "missing"}, 0},
	} {
		if got := count(c.req); got != c.want {
			t.Errorf("%s: %d hits, want %d", name, got, c.want)
		}
	}
	p := search(t, x, SearchRequest{Query: "needle", Limit: 5})
	if _, err := x.Search(t.Context(), SearchRequest{Query: "needle", Limit: 5, Harness: "codex", Cursor: p.NextCursor}); !errors.Is(err, ErrInvalid) {
		t.Fatal("cursor reused with other filters", err)
	}
	if _, err := x.Search(t.Context(), SearchRequest{Query: "needle", Limit: 5, Cursor: p.NextCursor + "x"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("tampered cursor", err)
	}
	backwards := until.Add(-time.Hour)
	if _, err := x.Search(t.Context(), SearchRequest{Query: "needle", Since: &until, Until: &backwards}); !errors.Is(err, ErrInvalid) {
		t.Fatal("empty range accepted", err)
	}
	if plan := queryPlan(t, x, SearchRequest{Query: "needle", Limit: 5}); strings.Contains(plan, "TEMP B-TREE") {
		t.Fatal("search sorts in a temp b-tree:", plan)
	}
}

// queryPlan explains the query Search would run for req.
func queryPlan(t *testing.T, x *Index, req SearchRequest) string {
	t.Helper()
	if req.Limit == 0 {
		req.Limit = SearchDefaultLimit
	}
	q, args := searchSQL(req, 0)
	rows, err := x.db.Query("EXPLAIN QUERY PLAN "+q, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatal(err)
		}
		plan += detail + "; "
	}
	return plan
}

func TestRunNotifiesAndStops(t *testing.T) {
	s := lake(t)
	uid := ingest(t, s, "run")
	x := openIndex(t, s)
	x.Interval = time.Hour
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { x.Run(ctx); close(done) }()
	x.waitPasses(1)
	publish(t, s, uid, events(1, func(int) string { return "arrives later" }))
	x.Notify(uid)
	x.waitPasses(2)
	if p := search(t, x, SearchRequest{Query: "arrives"}); len(p.Items) != 1 {
		t.Fatal("notify did not index")
	}
	cancel()
	<-done
}

func TestSnippetOffsets(t *testing.T) {
	long := strings.Repeat("ab\n", 200) + "Ünïcode MATCH" + strings.Repeat("\tz", 200)
	s, at, n := snippet(long, "ünïcode match")
	if !strings.HasPrefix(s, "…") || !strings.HasSuffix(s, "…") || s[at:at+n] != "Ünïcode MATCH" || strings.ContainsAny(s, "\n\t") {
		t.Fatalf("%q %d %d", s, at, n)
	}
	if s, _, n := snippet("short", "absent"); s != "short" || n != 0 {
		t.Fatal("unplaced", s, n)
	}
}

func removeFile(s *api.Server, uid string) error {
	return os.Remove(filepath.Join(s.Normalized, uid+normalize.EventsExt))
}

func TestStructuredFilters(t *testing.T) {
	s := lake(t)
	a := ingestAs(t, s, "codex", "a", "git@example.com:org/one.git")
	b := ingestAs(t, s, "claude", "b", "")
	str := func(v string) *string { return &v }
	yes, no := true, false
	build := func(prefix string) []normalize.Event {
		evs := events(40, func(i int) string { return fmt.Sprint(prefix, " event ", i) })
		for i := range evs {
			switch i % 4 {
			case 1:
				evs[i].Actor, evs[i].EventType = normalize.ActorAssistant, normalize.EventToolCall
				evs[i].Tool = normalize.Tool{Name: str("Bash")}
				evs[i].RawType = "function_call"
			case 2:
				evs[i].Actor, evs[i].EventType = normalize.ActorTool, normalize.EventToolResult
				evs[i].Tool = normalize.Tool{Name: str("Bash"), IsError: map[bool]*bool{true: &yes, false: &no}[i%8 == 2]}
			case 3:
				evs[i].Actor, evs[i].EventType, evs[i].ContentText = normalize.ActorHarness, normalize.EventUsage, nil
			}
		}
		return evs
	}
	publish(t, s, a, build("alpha git push"))
	publish(t, s, b, build("beta"))
	x := openIndex(t, s)
	pass(t, x)
	yesErr := true
	project := protocol.ProjectLinkID("git@example.com:org/one.git", root("x"))
	count := func(req SearchRequest) int {
		t.Helper()
		n := 0
		for {
			p := search(t, x, req)
			n += len(p.Items)
			if p.NextCursor == "" {
				return n
			}
			req.Cursor = p.NextCursor
		}
	}
	for name, c := range map[string]struct {
		req  SearchRequest
		want int
	}{
		"type only":           {SearchRequest{EventType: "tool_call", Limit: 3}, 20},
		"usage has no text":   {SearchRequest{EventType: "usage"}, 20},
		"actor":               {SearchRequest{Actor: "user"}, 20},
		"tool":                {SearchRequest{ToolName: "Bash"}, 40},
		"tool is exact":       {SearchRequest{ToolName: "bash"}, 0},
		"errors":              {SearchRequest{ToolError: &yesErr}, 10},
		"errors in project":   {SearchRequest{ToolError: &yesErr, Project: project}, 5},
		"raw type":            {SearchRequest{RawType: "function_call", Harness: "claude"}, 10},
		"text and type":       {SearchRequest{Query: "git push", EventType: "tool_call"}, 10},
		"text, type, project": {SearchRequest{Query: "event", ToolError: &yesErr, Unlinked: true}, 5},
		"combined none":       {SearchRequest{Query: "beta", Project: project}, 0},
	} {
		if got := count(c.req); got != c.want {
			t.Errorf("%s: %d hits, want %d", name, got, c.want)
		}
	}
	p := search(t, x, SearchRequest{EventType: "tool_call", Limit: 1})
	if h := p.Items[0]; h.MatchLen != 0 || !strings.Contains(h.Snippet, "event") || h.ToolName == nil || *h.ToolName != "Bash" {
		t.Fatalf("structured hit %+v", h)
	}
	for _, req := range []SearchRequest{
		{}, {Harness: "codex"}, {Project: project}, {EventType: "nonsense"}, {Actor: "robot"},
		{ToolName: strings.Repeat("x", 257)}, {RawType: "\xff\xfe"}, {Query: "ab", EventType: "message"},
	} {
		if _, err := x.Search(t.Context(), req); !errors.Is(err, ErrInvalid) {
			t.Errorf("accepted %+v: %v", req, err)
		}
	}
	for _, req := range []SearchRequest{{EventType: "tool_call"}, {ToolName: "Bash"}, {ToolError: &yesErr}, {Query: "needle", ToolName: "Bash"}} {
		if plan := queryPlan(t, x, req); strings.Contains(plan, "TEMP B-TREE") || strings.Contains(plan, "SCAN d") && !strings.Contains(plan, "USING") {
			t.Errorf("%+v plan: %s", req, plan)
		}
	}
}
