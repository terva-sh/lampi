package recall

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/normalize"
)

// Search limits.
const (
	SearchDefaultLimit = 50
	SearchMaxLimit     = 200
	// QueryMaxBytes caps the query text.
	QueryMaxBytes = 1024
	// QueryMinRunes is the shortest query: the trigram index cannot
	// match fewer characters.
	QueryMinRunes = 3
	// snippetRadius is how many bytes of context a snippet keeps on
	// each side of the match.
	snippetRadius = 120
)

// SearchRequest finds events by literal text, by what kind of event
// they are, or both. Query is matched as a case-insensitive substring
// of content_text, never parsed as FTS or SQL syntax. The event
// filters match exactly. A request needs text or at least one event
// filter; the session filters alone would list the corpus. The session
// filters match the web session list. Since is inclusive and Until
// exclusive, both on recorded time in UTC; an event with no recorded
// time is left out when either is set.
type SearchRequest struct {
	Query    string
	Harness  string
	Project  string
	Unlinked bool
	Since    *time.Time
	Until    *time.Time
	// Event filters. ToolError matches the recorded flag exactly: a
	// result whose harness did not say whether it failed matches
	// neither true nor false.
	EventType string
	Actor     string
	ToolName  string
	ToolError *bool
	RawType   string
	Limit     int
	Cursor    string
}

// EventTypes and Actors are the values the event filters accept: the
// normalized schema's vocabulary, plus unreadable for a line the index
// could not decode.
var (
	EventTypes = []string{normalize.EventMessage, normalize.EventToolCall, normalize.EventToolResult, normalize.EventUsage, normalize.EventCompaction, normalize.EventMeta, normalize.EventError, normalize.EventUnknown, "unreadable"}
	Actors     = []string{normalize.ActorUser, normalize.ActorAssistant, normalize.ActorSystem, normalize.ActorTool, normalize.ActorHarness}
)

// filterMaxBytes caps a free-form exact filter value.
const filterMaxBytes = 256

func (req SearchRequest) hasEventFilter() bool {
	return req.EventType != "" || req.Actor != "" || req.ToolName != "" || req.ToolError != nil || req.RawType != ""
}

func oneOf(v string, set []string) bool {
	for _, s := range set {
		if v == s {
			return true
		}
	}
	return false
}

// Hit is one matching event. Snippet is plain text around the match;
// MatchStart and MatchLen are byte offsets of the match in it, or
// MatchLen 0 when the match could not be placed.
type Hit struct {
	SessionUID string `json:"session_uid"`
	// NativeID and ProjectLabel are bounded display labels from the
	// catalog, as the session list shows them.
	NativeID     string  `json:"native_session_id"`
	ProjectLabel string  `json:"project_label"`
	Generation   int64   `json:"generation"`
	Position     int64   `json:"position"`
	Link         string  `json:"link"`
	Harness      string  `json:"harness"`
	ProjectID    string  `json:"project_id"`
	EventType    string  `json:"event_type"`
	Actor        string  `json:"actor"`
	ToolName     *string `json:"tool_name"`
	ToolError    *bool   `json:"tool_error"`
	RecordedAt   *string `json:"recorded_at"`
	Snippet      string  `json:"snippet"`
	MatchStart   int     `json:"match_start"`
	MatchLen     int     `json:"match_len"`
}

// SearchPage is one page of hits, newest indexed first. A page can
// hold fewer than the limit when sessions change during the read;
// next_cursor still continues it.
type SearchPage struct {
	Items      []Hit    `json:"items"`
	NextCursor string   `json:"next_cursor"`
	AsOf       string   `json:"as_of"`
	Coverage   Coverage `json:"coverage"`
}

type searchCursor struct {
	V      int    `json:"v"`
	Filter string `json:"f"`
	Before int64  `json:"b"`
}

func (req SearchRequest) fingerprint() string {
	req.Cursor = ""
	b, _ := json.Marshal(req)
	sum := sha256.Sum256(b)
	return base64.RawURLEncoding.EncodeToString(sum[:12])
}

// ValidateQuery reports whether q is a searchable literal.
func ValidateQuery(q string) error {
	if len(q) > QueryMaxBytes || !utf8.ValidString(q) || strings.ContainsRune(q, 0) {
		return ErrInvalid
	}
	if utf8.RuneCountInString(strings.TrimSpace(q)) < QueryMinRunes {
		return ErrInvalid
	}
	return nil
}

// ftsLiteral quotes q as one FTS5 string: every character inside the
// quotes is literal, and a quote is written twice.
func ftsLiteral(q string) string {
	return `"` + strings.ReplaceAll(q, `"`, `""`) + `"`
}

// Search runs req against the index. Hits whose session is no longer
// ready at that generation are dropped at read time, so a result never
// points at content that is stale, failed or purged.
func (x *Index) Search(ctx context.Context, req SearchRequest) (SearchPage, error) {
	page := SearchPage{Items: []Hit{}, AsOf: time.Now().UTC().Format(time.RFC3339Nano), Coverage: x.Coverage()}
	if req.Limit == 0 {
		req.Limit = SearchDefaultLimit
	}
	if req.Limit < 1 || req.Limit > SearchMaxLimit || len(req.Project) > 4096 || (req.Unlinked && req.Project != "") {
		return page, ErrInvalid
	}
	if req.Query != "" || !req.hasEventFilter() {
		if err := ValidateQuery(req.Query); err != nil {
			return page, err
		}
	}
	if (req.EventType != "" && !oneOf(req.EventType, EventTypes)) || (req.Actor != "" && !oneOf(req.Actor, Actors)) ||
		len(req.ToolName) > filterMaxBytes || len(req.RawType) > filterMaxBytes || !utf8.ValidString(req.ToolName) || !utf8.ValidString(req.RawType) {
		return page, ErrInvalid
	}
	if !validHarness(req.Harness) {
		return page, ErrInvalid
	}
	if req.Since != nil && req.Until != nil && !req.Since.Before(*req.Until) {
		return page, ErrInvalid
	}
	fp := req.fingerprint()
	var before int64 = -1
	if req.Cursor != "" {
		var c searchCursor
		body, err := x.reader.verifySigned(req.Cursor)
		if err != nil || json.Unmarshal(body, &c) != nil || c.V != 1 || c.Filter != fp || c.Before <= 0 {
			return page, ErrInvalid
		}
		before = c.Before
	}
	q, args := searchSQL(req, before)
	rows, err := x.db.QueryContext(ctx, q, args...)
	if err != nil {
		return page, err
	}
	type found struct {
		id  int64
		hit Hit
	}
	var hits []found
	for rows.Next() {
		var f found
		var tool sql.NullString
		var toolErr sql.NullBool
		var rec sql.NullInt64
		var content sql.NullString
		h := &f.hit
		if err := rows.Scan(&f.id, &h.SessionUID, &h.Generation, &h.Position, &h.Harness, &h.ProjectID, &h.EventType, &h.Actor, &tool, &toolErr, &rec, &content); err != nil {
			rows.Close()
			return page, err
		}
		if tool.Valid {
			h.ToolName = &tool.String
		}
		if toolErr.Valid {
			h.ToolError = &toolErr.Bool
		}
		if rec.Valid {
			s := time.Unix(0, rec.Int64).UTC().Format(time.RFC3339Nano)
			h.RecordedAt = &s
		}
		h.Snippet, h.MatchStart, h.MatchLen = snippet(content.String, req.Query)
		h.Link = EventLink(h.SessionUID, h.Generation, h.Position)
		hits = append(hits, f)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, err
	}
	more := len(hits) > req.Limit
	if more {
		hits = hits[:req.Limit]
	}
	current := map[string]int64{}
	labels := map[string]catalog.SessionSummary{}
	for _, f := range hits {
		uid := f.hit.SessionUID
		if _, seen := current[uid]; seen {
			continue
		}
		pub, err := x.reader.publication(ctx, uid)
		switch {
		case err == nil && pub.State == "ready":
			current[uid] = pub.Gen
		case err == nil || errors.Is(err, ErrNotFound):
			current[uid] = -1
			continue
		default:
			return page, err
		}
		summary, err := x.reader.catalog.DashboardSession(ctx, uid)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return page, err
		}
		labels[uid] = summary
	}
	for _, f := range hits {
		if current[f.hit.SessionUID] == f.hit.Generation {
			l := labels[f.hit.SessionUID]
			f.hit.NativeID, f.hit.ProjectLabel = l.NativeID, l.ProjectLabel
			page.Items = append(page.Items, f.hit)
		}
	}
	if more {
		page.NextCursor = x.reader.signBody(searchCursor{V: 1, Filter: fp, Before: hits[len(hits)-1].id})
	}
	return page, nil
}

// searchSQL builds the page query. It is separate so tests can check
// its plan.
func searchSQL(req SearchRequest, before int64) (string, []any) {
	// With text, FTS5 walks its rowids newest first. Without, the docs
	// primary key does, through an event filter's index.
	// The key must be the table SQLite walks, or it sorts every match.
	from, key := `fts JOIN docs d ON d.id=fts.rowid`, "fts.rowid"
	var where []string
	var args []any
	if req.Query != "" {
		where = append(where, "fts MATCH ?")
		args = append(args, ftsLiteral(req.Query))
	} else {
		from, key = `docs d`, "d.id"
	}
	if before > 0 {
		where = append(where, key+"<?")
		args = append(args, before)
	}
	for _, f := range []struct {
		col, val string
	}{{"d.event_type", req.EventType}, {"d.actor", req.Actor}, {"d.tool_name", req.ToolName}, {"d.raw_type", req.RawType}} {
		if f.val != "" {
			where = append(where, f.col+"=?")
			args = append(args, f.val)
		}
	}
	if req.ToolError != nil {
		where = append(where, "d.tool_error=?")
		args = append(args, *req.ToolError)
	}
	if req.Harness != "" {
		where = append(where, "d.harness=?")
		args = append(args, req.Harness)
	}
	if req.Project != "" {
		where = append(where, "d.project_id=?")
		args = append(args, req.Project)
	} else if req.Unlinked {
		where = append(where, "d.project_id=''")
	}
	if req.Since != nil {
		where = append(where, "d.recorded_ns>=?")
		args = append(args, req.Since.UnixNano())
	}
	if req.Until != nil {
		where = append(where, "d.recorded_ns<?")
		args = append(args, req.Until.UnixNano())
	}
	args = append(args, req.Limit+1)
	q := `SELECT d.id,d.session_uid,d.gen,d.pos,d.harness,d.project_id,d.event_type,d.actor,d.tool_name,d.tool_error,d.recorded_ns,d.content
		FROM ` + from + ` JOIN indexed i ON i.session_uid=d.session_uid AND i.gen=d.gen
		WHERE ` + strings.Join(append(where, "1=1"), " AND ") + ` ORDER BY ` + key + ` DESC LIMIT ?`
	return q, args
}

func validHarness(h string) bool {
	switch h {
	case "", "terva", "claude", "codex", "opencode", "cursor", "cursor-cli":
		return true
	}
	return false
}

// snippet cuts plain text around the first case-insensitive occurrence
// of q in content. Line breaks and tabs become spaces, one byte for one
// byte, so the offsets hold. When q cannot be placed, the snippet is the
// start of content with no match marked.
func snippet(content, q string) (string, int, int) {
	start, end := foldIndex(content, q)
	if start < 0 {
		return flatten(truncate(content, 2*snippetRadius)), 0, 0
	}
	from := max(0, start-snippetRadius)
	for from > 0 && !utf8.RuneStart(content[from]) {
		from--
	}
	to := min(len(content), end+snippetRadius)
	for to < len(content) && !utf8.RuneStart(content[to]) {
		to++
	}
	prefix, suffix := "", ""
	if from > 0 {
		prefix = "…"
	}
	if to < len(content) {
		suffix = "…"
	}
	return prefix + flatten(content[from:to]) + suffix, len(prefix) + start - from, end - start
}

func flatten(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		return r
	}, s)
}

// foldIndex finds q in s under simple case folding and returns the
// byte range in s, or -1.
func foldIndex(s, q string) (int, int) {
	qr := []rune(q)
	if len(qr) == 0 {
		return -1, -1
	}
	for i := 0; i < len(s); {
		j, k := i, 0
		for k < len(qr) && j < len(s) {
			r, n := utf8.DecodeRuneInString(s[j:])
			if !equalFold(r, qr[k]) {
				break
			}
			j += n
			k++
		}
		if k == len(qr) {
			return i, j
		}
		_, n := utf8.DecodeRuneInString(s[i:])
		i += n
	}
	return -1, -1
}

func equalFold(a, b rune) bool {
	if a == b {
		return true
	}
	return unicode.ToLower(a) == unicode.ToLower(b)
}
