// Package recall is the query layer shared by the browser API and the
// MCP server: event pages, search, deep links and copy-out. Adapters
// parse their own inputs and call it; they do not read the catalog or
// the derived files themselves.
package recall

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/normalize"
)

// Limits on one event page. A page stops at whichever is reached
// first; it always holds at least one event.
const (
	DefaultLimit = 100
	MaxLimit     = 200
	// PageBytes caps the encoded items of one page.
	PageBytes = 1 << 20
	// ContentPreview is how much content_text one event carries in a
	// page. Longer text is cut at a rune boundary and marked.
	ContentPreview = 32 << 10
	// ExtraBytes caps the encoded extra object of one event. A larger
	// one is left out and marked.
	ExtraBytes = 16 << 10
	// MaxLine is the longest JSONL line decoded. A longer line becomes
	// a placeholder that keeps its position.
	MaxLine = 16 << 20
)

var (
	// ErrInvalid is a malformed request: a bad position, limit or cursor.
	ErrInvalid = errors.New("recall: invalid request")
	// ErrNotFound is an unknown session.
	ErrNotFound = errors.New("recall: session not found")
	// ErrGenerationChanged means the request was pinned to a
	// generation that is no longer the published one. Reload.
	ErrGenerationChanged = errors.New("recall: generation changed")
)

// UnavailableError is a session with no readable published output:
// pending, failed, unknown, or ready with the file missing.
type UnavailableError struct{ State string }

func (e UnavailableError) Error() string { return "recall: transcript unavailable: " + e.State }

// Reader serves the published normalized JSONL. It never starts
// normalization and never opens the CAS.
type Reader struct {
	catalog    *catalog.Catalog
	normalized string
	key        []byte
}

// NewReader reads catalog state from cat and derived files from
// normalized. Cursors it issues are signed with a key made here, so a
// restart invalidates them, as it does browser sessions.
func NewReader(cat *catalog.Catalog, normalized string) *Reader {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}
	return &Reader{catalog: cat, normalized: normalized, key: key}
}

// EventRequest selects one page. From is the first position, counted
// from zero, and is ignored when Cursor is set. Pinned asks for
// generation Gen only; zero is a real generation.
type EventRequest struct {
	From   int64
	Limit  int
	Cursor string
	Gen    int64
	Pinned bool
}

// EventPage is a run of consecutive events from one generation.
type EventPage struct {
	SessionUID string      `json:"session_uid"`
	Generation int64       `json:"generation"`
	Head       string      `json:"head_sha256"`
	From       int64       `json:"from"`
	Items      []EventItem `json:"items"`
	NextCursor string      `json:"next_cursor"`
	PrevFrom   *int64      `json:"prev_from"`
	End        bool        `json:"end"`
	AsOf       string      `json:"as_of"`
}

// EventItem is one event with its address and what was left out of it.
type EventItem struct {
	Position     int64            `json:"position"`
	Link         string           `json:"link"`
	ContentBytes int              `json:"content_bytes"`
	Truncated    bool             `json:"content_truncated"`
	ExtraOmitted bool             `json:"extra_omitted"`
	Opaque       bool             `json:"opaque_content"`
	Oversized    bool             `json:"oversized"`
	Unreadable   bool             `json:"unreadable"`
	Event        *normalize.Event `json:"event"`
}

// EventLink is the viewer address of one event in one generation.
func EventLink(uid string, gen, pos int64) string {
	q := url.Values{}
	q.Set("gen", strconv.FormatInt(gen, 10))
	q.Set("at", strconv.FormatInt(pos, 10))
	return "/sessions/" + url.PathEscape(uid) + "/transcript?" + q.Encode() + "#e-" + strconv.FormatInt(pos, 10)
}

// snapshot is an open events file known to hold pub's generation.
type snapshot struct {
	ev    *normalize.EventsFile
	pub   catalog.Publication
	size  int64
	mtime int64
}

func (s *snapshot) Close() error { return s.ev.Close() }

// lines returns the events from pos as a buffered reader, having
// skipped to it: from the start of the frame holding it in a
// compressed file, or from off, a signed cursor's byte offset at pos in
// a plain one. at is the position reached, below pos past the end, and
// consumed the plain file's byte offset there; zero in a compressed
// file, which a cursor finds by position.
func (s *snapshot) lines(ctx context.Context, pos, off int64) (br *bufio.Reader, at, consumed int64, err error) {
	r, first, err := s.ev.From(pos, off)
	if err != nil {
		return nil, 0, 0, err
	}
	br = bufio.NewReaderSize(r, 64<<10)
	if !s.ev.Compressed && first == pos {
		consumed = off
	}
	at = first
	if first < pos {
		n, skipped, err := skipLines(ctx, br, pos-first)
		if err != nil {
			return nil, 0, 0, err
		}
		at += skipped
		consumed += n
	}
	if s.ev.Compressed {
		consumed = 0
	}
	return br, at, consumed, nil
}

// open pins the published generation of uid. Publication is read,
// the file opened, and publication read again. A worker bumps
// normalize_gen before it replaces the file, and removes the file
// before it records a failure, so an unchanged ready state on both
// sides of the open means the descriptor holds that generation. A
// rename after the open does not change what the descriptor reads.
func (r *Reader) open(ctx context.Context, uid string) (*snapshot, error) {
	if !validUID(uid) {
		return nil, ErrInvalid
	}
	for attempt := 0; ; attempt++ {
		before, err := r.publication(ctx, uid)
		if err != nil {
			return nil, err
		}
		if before.State != "ready" {
			return nil, UnavailableError{before.State}
		}
		ev, err := normalize.OpenEvents(r.normalized, uid)
		if errors.Is(err, os.ErrNotExist) {
			after, perr := r.publication(ctx, uid)
			if perr != nil {
				return nil, perr
			}
			if after != before && attempt == 0 {
				continue
			}
			if after.State != "ready" {
				return nil, UnavailableError{after.State}
			}
			return nil, UnavailableError{"missing"}
		}
		if err != nil {
			return nil, err
		}
		after, err := r.publication(ctx, uid)
		if err != nil {
			ev.Close()
			return nil, err
		}
		if after != before {
			ev.Close()
			if attempt == 0 {
				continue
			}
			if after.State != "ready" {
				return nil, UnavailableError{after.State}
			}
			return nil, ErrGenerationChanged
		}
		return &snapshot{ev: ev, pub: before, size: ev.Size, mtime: ev.MTime}, nil
	}
}

func (r *Reader) publication(ctx context.Context, uid string) (catalog.Publication, error) {
	p, err := r.catalog.Publication(ctx, uid)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return p, ErrNotFound
		}
		return p, err
	}
	return p, nil
}

// cursor continues a page run. It is signed, so a client cannot move
// Off away from the line that Pos names.
type cursor struct {
	V     int    `json:"v"`
	UID   string `json:"u"`
	Gen   int64  `json:"g"`
	Head  string `json:"h"`
	Size  int64  `json:"s"`
	MTime int64  `json:"m"`
	Pos   int64  `json:"p"`
	Off   int64  `json:"o"`
}

func (r *Reader) sign(c cursor) string { return r.signBody(c) }

func (r *Reader) verify(s string) (cursor, error) {
	var c cursor
	body, err := r.verifySigned(s)
	if err != nil {
		return c, err
	}
	if json.Unmarshal(body, &c) != nil || c.V != 1 {
		return c, ErrInvalid
	}
	return c, nil
}

// signBody encodes v as JSON with an HMAC under the process key.
func (r *Reader) signBody(v any) string {
	b, _ := json.Marshal(v)
	mac := hmac.New(sha256.New, r.key)
	mac.Write(b)
	return base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// verifySigned returns the JSON body of a token signBody made.
func (r *Reader) verifySigned(s string) ([]byte, error) {
	if len(s) > 2048 {
		return nil, ErrInvalid
	}
	i := strings.IndexByte(s, '.')
	if i < 0 {
		return nil, ErrInvalid
	}
	body, err := base64.RawURLEncoding.DecodeString(s[:i])
	if err != nil {
		return nil, ErrInvalid
	}
	sum, err := base64.RawURLEncoding.DecodeString(s[i+1:])
	if err != nil {
		return nil, ErrInvalid
	}
	mac := hmac.New(sha256.New, r.key)
	mac.Write(body)
	if !hmac.Equal(sum, mac.Sum(nil)) {
		return nil, ErrInvalid
	}
	return body, nil
}

// Events reads one page of uid's published events.
func (r *Reader) Events(ctx context.Context, scope catalog.Scope, uid string, req EventRequest) (EventPage, error) {
	if err := r.inScope(ctx, scope, uid); err != nil {
		return EventPage{}, err
	}
	if req.Limit == 0 {
		req.Limit = DefaultLimit
	}
	if req.Limit < 1 || req.Limit > MaxLimit || req.From < 0 || req.Gen < 0 {
		return EventPage{}, ErrInvalid
	}
	var cur cursor
	if req.Cursor != "" {
		var err error
		if cur, err = r.verify(req.Cursor); err != nil || cur.UID != uid {
			return EventPage{}, ErrInvalid
		}
	}
	snap, err := r.open(ctx, uid)
	if err != nil {
		return EventPage{}, err
	}
	defer snap.Close()
	if req.Pinned && req.Gen != snap.pub.Gen {
		return EventPage{}, ErrGenerationChanged
	}
	pos, off := req.From, int64(0)
	if req.Cursor != "" {
		if cur.Gen != snap.pub.Gen || cur.Head != snap.pub.Head || cur.Size != snap.size || cur.MTime != snap.mtime {
			return EventPage{}, ErrGenerationChanged
		}
		if cur.Off < 0 || cur.Off > snap.size || cur.Pos < 0 {
			return EventPage{}, ErrInvalid
		}
		pos, off = cur.Pos, cur.Off
	}
	// Past the end, at is short of pos: an empty last page, not an
	// error, so a link to a shortened session still lands somewhere.
	br, pos, off, err := snap.lines(ctx, pos, off)
	if err != nil {
		return EventPage{}, err
	}
	page := EventPage{SessionUID: uid, Generation: snap.pub.Gen, Head: snap.pub.Head, From: pos, Items: []EventItem{}, AsOf: time.Now().UTC().Format(time.RFC3339Nano)}
	if pos > 0 {
		prev := max(0, pos-int64(req.Limit))
		page.PrevFrom = &prev
	}
	used := 0
	for len(page.Items) < req.Limit {
		if err := ctx.Err(); err != nil {
			return EventPage{}, err
		}
		line, n, err := readLine(br, MaxLine)
		if err == io.EOF && n == 0 {
			page.End = true
			break
		}
		if err != nil && err != io.EOF {
			return EventPage{}, err
		}
		item := decodeItem(uid, snap.pub.Gen, pos, line)
		b, _ := json.Marshal(item)
		if used+len(b) > PageBytes && len(page.Items) > 0 {
			break
		}
		used += len(b)
		page.Items = append(page.Items, item)
		pos++
		if !snap.ev.Compressed {
			off += n
		}
		if err == io.EOF {
			page.End = true
			break
		}
	}
	if !page.End {
		// Peek so a page that ends exactly at the last event says so.
		if _, err := br.Peek(1); err == io.EOF {
			page.End = true
		}
	}
	if !page.End {
		page.NextCursor = r.sign(cursor{V: 1, UID: uid, Gen: snap.pub.Gen, Head: snap.pub.Head, Size: snap.size, MTime: snap.mtime, Pos: pos, Off: off})
	}
	return page, nil
}

// decodeItem turns one JSONL line into a bounded item. A nil line is
// an oversized one.
func decodeItem(uid string, gen, pos int64, line []byte) EventItem {
	item := EventItem{Position: pos, Link: EventLink(uid, gen, pos)}
	if line == nil {
		item.Oversized = true
		return item
	}
	var ev normalize.Event
	if err := json.Unmarshal(line, &ev); err != nil {
		item.Unreadable = true
		return item
	}
	if ev.ContentText != nil {
		item.ContentBytes = len(*ev.ContentText)
		if len(*ev.ContentText) > ContentPreview {
			cut := truncate(*ev.ContentText, ContentPreview)
			ev.ContentText = &cut
			item.Truncated = true
		}
	}
	if ev.Extra != nil {
		item.Opaque = stripOpaque(ev.Extra, 0)
		if b, _ := json.Marshal(ev.Extra); len(b) > ExtraBytes {
			ev.Extra = nil
			item.ExtraOmitted = true
		}
	}
	item.Event = &ev
	return item
}

// opaqueKey matches the extra fields the normalizers copy through
// without decrypting. The viewer does not show them.
var opaqueKey = regexp.MustCompile(`(?i)encrypted|cipher|sealed`)

// stripOpaque removes opaque encrypted values from m, to a bounded
// depth, and reports whether it removed any.
func stripOpaque(m map[string]any, depth int) bool {
	if depth > 8 {
		return false
	}
	found := false
	for k, v := range m {
		if opaqueKey.MatchString(k) {
			delete(m, k)
			found = true
			continue
		}
		switch t := v.(type) {
		case map[string]any:
			if stripOpaque(t, depth+1) {
				found = true
			}
		case []any:
			for _, e := range t {
				if sub, ok := e.(map[string]any); ok && stripOpaque(sub, depth+1) {
					found = true
				}
			}
		}
	}
	return found
}

// truncate cuts s to at most n bytes at a rune boundary.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// readLine reads one line without its newline and reports the bytes
// consumed. A line longer than max is consumed and returned as nil.
// A final line without a newline ends with io.EOF.
func readLine(br *bufio.Reader, max int) ([]byte, int64, error) {
	var buf []byte
	var n int64
	over := false
	for {
		chunk, err := br.ReadSlice('\n')
		n += int64(len(chunk))
		if !over {
			if len(buf)+len(chunk) > max+1 {
				over, buf = true, nil
			} else {
				buf = append(buf, chunk...)
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil && err != io.EOF {
			return nil, n, err
		}
		if over {
			return nil, n, err
		}
		return bytes.TrimSuffix(buf, []byte("\n")), n, err
	}
}

// skipLines consumes n lines and reports the bytes and lines consumed.
// It stops early at the end of the file.
func skipLines(ctx context.Context, br *bufio.Reader, n int64) (int64, int64, error) {
	var bytesRead, lines int64
	for lines < n {
		if lines%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return 0, 0, err
			}
		}
		chunk, err := br.ReadSlice('\n')
		bytesRead += int64(len(chunk))
		if err == bufio.ErrBufferFull {
			continue
		}
		if err == io.EOF {
			if len(chunk) > 0 {
				lines++
			}
			return bytesRead, lines, nil
		}
		if err != nil {
			return 0, 0, err
		}
		lines++
	}
	return bytesRead, lines, nil
}

// inScope is ErrNotFound for a session outside scope, the answer a
// session that is not stored gets, so a caller learns nothing about
// sessions in bays it does not read.
func (r *Reader) inScope(ctx context.Context, scope catalog.Scope, uid string) error {
	if !validUID(uid) {
		return ErrInvalid
	}
	ok, err := r.catalog.SessionInScope(ctx, scope, uid)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

func validUID(uid string) bool {
	return uid != "" && len(uid) <= 128 && filepath.Base(uid) == uid && uid != "." && uid != ".."
}
