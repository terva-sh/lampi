package recall

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/normalize"
)

// IndexFile is the search index under the lake data directory. It is
// derived from the published normalized JSONL. Deleting it while serve
// is stopped rebuilds it on the next start; backups leave it out.
const IndexFile = "search.db"

// indexVersion is the search.db schema. A file with any other version
// is deleted and rebuilt rather than migrated: it holds nothing that
// the derived files do not. Version 2 is version 1 with incremental
// auto-vacuum, which a file takes only before it has any pages. Version
// 3 keys rows by position and a signature of their fields, not by
// generation, so a new generation rewrites only the rows that changed.
// Version 4 stores no recorded time for an event whose source had none,
// and rebuilds a file that rewriting those rows grew (TKT-01M3NENNN8).
const indexVersion = 4

// mergePages bounds the full-text merge after a pass, in leaf pages.
// Rows deleted from an FTS5 index stay in its segments until they
// merge. When every session was re-indexed whole at every sync, that
// grew the file to about four times its live size (TKT-01M3KC2DD).
//
// After a pass that only added rows, the merge is FTS5's ordinary one,
// which merges a level once it holds enough segments. After a pass that
// deleted rows, it is forced, which merges whatever is there and so
// drops the deleted rows, until a forced merge finds nothing left. A
// forced merge after every pass would rewrite mergePages of the index
// each time, about 8 MiB, even when a pass appended a few events.
const mergePages = 2000

// IndexContentMax is how much of one event's content_text is indexed
// and searchable. Text past it is still in the transcript.
const IndexContentMax = 256 << 10

const indexSchema = `
CREATE TABLE indexed (
	session_uid TEXT PRIMARY KEY,
	gen INTEGER NOT NULL,
	head TEXT NOT NULL,
	events INTEGER NOT NULL,
	indexed_at TEXT NOT NULL
);
CREATE TABLE docs (
	id INTEGER PRIMARY KEY,
	session_uid TEXT NOT NULL,
	pos INTEGER NOT NULL,
	sig INTEGER NOT NULL,
	harness TEXT NOT NULL,
	project_id TEXT NOT NULL,
	event_type TEXT NOT NULL,
	actor TEXT NOT NULL,
	tool_name TEXT,
	tool_error INTEGER,
	raw_type TEXT NOT NULL,
	recorded_ns INTEGER,
	content TEXT
);
CREATE INDEX docs_session ON docs(session_uid, pos);
CREATE INDEX docs_type ON docs(event_type, id);
CREATE INDEX docs_tool ON docs(tool_name, id);
CREATE INDEX docs_error ON docs(tool_error, id);
CREATE VIRTUAL TABLE fts USING fts5(content, content='docs', content_rowid='id', tokenize='trigram');
CREATE TRIGGER docs_ai AFTER INSERT ON docs BEGIN
	INSERT INTO fts(rowid, content) VALUES (new.id, new.content);
END;
CREATE TRIGGER docs_ad AFTER DELETE ON docs BEGIN
	INSERT INTO fts(fts, rowid, content) VALUES ('delete', old.id, old.content);
END;
`

// Coverage says how much of the lake search can see. Ready counts
// sessions whose normalization is ready. Indexed counts those whose
// current generation is searchable. Failed counts ready sessions the
// indexer could not read. Behind is the rest: ready, not yet indexed.
type Coverage struct {
	Ready         int    `json:"ready_sessions"`
	Indexed       int    `json:"indexed_sessions"`
	Behind        int    `json:"behind_sessions"`
	Failed        int    `json:"failed_sessions"`
	LastReconcile string `json:"last_reconcile"`
}

// Index keeps search.db in step with the catalog's published
// generations. The catalog is the work queue: each pass compares what
// is published with what is indexed, so a crash or restart loses
// nothing that the next pass does not find.
type Index struct {
	db     *sql.DB
	reader *Reader
	Log    *slog.Logger
	// Interval is the longest wait between passes. Notify starts one
	// sooner.
	Interval time.Duration

	wake chan struct{}
	mu   sync.Mutex
	cov  Coverage
	// failed holds the generation that could not be indexed, so it is
	// not read again until a newer one is published.
	failed map[string]int64
	// merging is set from a pass's first write until a reclaim finds
	// nothing more to merge, so a pass that stops part way or a reclaim
	// that fails leaves the work to the next pass. It starts set, as a
	// process that stopped may have left work. Only Pass reads and
	// writes it.
	merging bool
	// deleted is set by a pass that deleted rows, and cleared when a
	// forced merge finds nothing left to merge. Only Pass reads and
	// writes it.
	deleted bool
	// beforeReclaim, when set, runs before a pass reclaims. Tests use
	// it to stop a pass there.
	beforeReclaim func()
	// passes counts finished passes, for tests.
	passes int
	passed *sync.Cond
}

// OpenIndex opens or creates the index at path. reader supplies the
// catalog and the generation-pinned files.
func OpenIndex(path string, reader *Reader) (*Index, error) {
	db, err := openIndexDB(path)
	if err != nil {
		return nil, err
	}
	x := &Index{db: db, reader: reader, Interval: 5 * time.Minute, wake: make(chan struct{}, 1), failed: map[string]int64{}, merging: true}
	x.passed = sync.NewCond(&x.mu)
	return x, nil
}

func indexDSN(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	q := url.Values{}
	for _, p := range []string{"auto_vacuum(INCREMENTAL)", "busy_timeout(5000)", "journal_mode(WAL)", "synchronous(NORMAL)"} {
		q.Add("_pragma", p)
	}
	q.Set("_txlock", "immediate")
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs), RawQuery: q.Encode()}).String(), nil
}

func openIndexDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		dsn, err := indexDSN(path)
		if err != nil {
			return nil, fmt.Errorf("search: %w", err)
		}
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			return nil, fmt.Errorf("search: %w", err)
		}
		db.SetMaxOpenConns(4)
		var v int
		if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
			db.Close()
			return nil, fmt.Errorf("search: %w", err)
		}
		if v == indexVersion {
			return db, os.Chmod(path, 0o600)
		}
		if v == 0 {
			var tables int
			if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master`).Scan(&tables); err != nil {
				db.Close()
				return nil, fmt.Errorf("search: %w", err)
			}
			if tables == 0 {
				if _, err := db.Exec(indexSchema + fmt.Sprintf("PRAGMA user_version=%d;", indexVersion)); err != nil {
					db.Close()
					return nil, fmt.Errorf("search: create: %w", err)
				}
				return db, os.Chmod(path, 0o600)
			}
		}
		// Another version, or tables with no version: derived data, so
		// start over rather than guess at its shape.
		db.Close()
		if err := removeIndexFiles(path); err != nil {
			return nil, err
		}
	}
	return nil, errors.New("search: index could not be recreated")
}

func removeIndexFiles(path string) error {
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("search: %w", err)
		}
	}
	return nil
}

// Close releases the index database. Stop Run first.
func (x *Index) Close() error { return x.db.Close() }

// Notify asks for a pass soon. Publication and normalize failures call
// it; it never blocks.
func (x *Index) Notify(string) {
	select {
	case x.wake <- struct{}{}:
	default:
	}
}

// Coverage is the state after the latest pass, updated as sessions
// are indexed.
func (x *Index) Coverage() Coverage {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.cov
}

// Run passes until ctx ends: one at once, then on Notify or after
// Interval. A notify during a pass starts another after it.
func (x *Index) Run(ctx context.Context) {
	for {
		if err := x.Pass(ctx); err != nil && ctx.Err() == nil {
			x.logger().Warn("search index pass failed", "err", err.Error())
		}
		t := time.NewTimer(x.Interval)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-x.wake:
			t.Stop()
			// Let a burst of publications settle into one pass.
			select {
			case <-ctx.Done():
				return
			case <-time.After(200 * time.Millisecond):
			}
		case <-t.C:
		}
	}
}

func (x *Index) logger() *slog.Logger {
	if x.Log != nil {
		return x.Log
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type indexedRow struct {
	gen  int64
	head string
}

// Pass brings the index in step with the catalog once: it indexes each
// ready session whose published generation is not the indexed one,
// and drops sessions that are gone, failed or never published.
func (x *Index) Pass(ctx context.Context) error {
	sessions, err := x.reader.catalog.PublishedSessions(ctx)
	if err != nil {
		return err
	}
	have := map[string]indexedRow{}
	rows, err := x.db.QueryContext(ctx, `SELECT session_uid,gen,head FROM indexed`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var uid string
		var r indexedRow
		if err := rows.Scan(&uid, &r.gen, &r.head); err != nil {
			rows.Close()
			return err
		}
		have[uid] = r
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	cov := Coverage{LastReconcile: time.Now().UTC().Format(time.RFC3339Nano)}
	var todo []catalog.PublishedSession
	keep := map[string]bool{}
	x.mu.Lock()
	for _, s := range sessions {
		switch s.State {
		case "ready":
			cov.Ready++
			keep[s.UID] = true
			if r, ok := have[s.UID]; ok && r.gen == s.Gen && r.head == s.Head {
				cov.Indexed++
				delete(x.failed, s.UID)
				continue
			}
			if g, ok := x.failed[s.UID]; ok && g == s.Gen {
				cov.Failed++
				continue
			}
			delete(x.failed, s.UID)
			todo = append(todo, s)
		case "pending":
			// A newer generation is on its way. Its old rows stay until
			// it lands; queries hide them meanwhile.
			keep[s.UID] = true
		}
	}
	cov.Behind = len(todo)
	x.cov = cov
	x.mu.Unlock()
	// Pending before the first write, so a pass that stops part way
	// still leaves the reclaim to the next one.
	if len(todo) > 0 {
		x.merging = true
	}
	for uid := range have {
		if keep[uid] {
			continue
		}
		x.merging, x.deleted = true, true
		if err := x.remove(ctx, uid); err != nil {
			return err
		}
	}
	for _, s := range todo {
		if err := ctx.Err(); err != nil {
			return err
		}
		deleted, err := x.indexSession(ctx, s)
		if deleted > 0 {
			x.deleted = true
		}
		x.mu.Lock()
		x.cov.Behind--
		switch {
		case err == nil:
			x.cov.Indexed++
		case ctx.Err() != nil:
			x.cov.Behind++
		default:
			var unavailable UnavailableError
			changed := errors.Is(err, ErrGenerationChanged) || (errors.As(err, &unavailable) && unavailable.State != "missing") || errors.Is(err, ErrNotFound)
			if changed {
				// Moved on while we read; the next pass picks it up.
				x.cov.Behind++
				x.Notify(s.UID)
			} else {
				x.cov.Failed++
				x.failed[s.UID] = s.Gen
				x.logger().Warn("search index skipped a session", "session_uid", s.UID, "gen", s.Gen, "err", err.Error())
			}
		}
		x.mu.Unlock()
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	if x.merging {
		if x.beforeReclaim != nil {
			x.beforeReclaim()
		}
		more, err := x.reclaim(ctx, x.deleted)
		x.merging = more
		if err == nil && !more {
			x.deleted = false
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			x.logger().Warn("search index reclaim failed", "err", err.Error())
		}
	}
	x.mu.Lock()
	x.passes++
	x.passed.Broadcast()
	x.mu.Unlock()
	return nil
}

// indexSession brings uid's rows to the published generation of s in
// one transaction. A row whose searchable fields did not change keeps
// its place in the full-text index; only changed, new and removed rows
// are written. A session that grew by a few events at a sync writes
// those events, not the whole session again (TKT-01M3KC2DD). Queries
// see the old rows until the commit.
func (x *Index) indexSession(ctx context.Context, s catalog.PublishedSession) (deleted int64, err error) {
	snap, err := x.reader.open(ctx, s.UID)
	if err != nil {
		return 0, err
	}
	defer snap.Close()
	gen, head := snap.pub.Gen, snap.pub.Head
	old, err := x.rowSigs(ctx, s.UID)
	if err != nil {
		return 0, err
	}
	tx, err := x.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	w := docWriter{tx: tx}
	br, _, _, err := snap.lines(ctx, 0, 0)
	if err != nil {
		return 0, err
	}
	var pos int64
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		line, n, rerr := readLine(br, MaxLine)
		if rerr == io.EOF && n == 0 {
			break
		}
		if rerr != nil && rerr != io.EOF {
			return 0, rerr
		}
		row := docRow{uid: s.UID, pos: pos, harness: s.Harness, project: s.ProjectID}
		row.fill(line)
		sig := row.sig()
		prev, had := old[pos]
		pos++
		if had && prev.sig == sig {
			if rerr == io.EOF {
				break
			}
			continue
		}
		if had {
			w.del = append(w.del, prev.id)
			deleted++
		}
		if err := w.insert(ctx, &row, sig); err != nil {
			return 0, err
		}
		if rerr == io.EOF {
			break
		}
	}
	if err := w.flush(ctx); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM docs WHERE session_uid=? AND pos>=?`, s.UID, pos)
	if err != nil {
		return 0, err
	}
	gone, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	deleted += gone
	now, err := x.reader.publication(ctx, s.UID)
	if err != nil {
		return 0, err
	}
	if now != snap.pub {
		return 0, ErrGenerationChanged
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO indexed(session_uid,gen,head,events,indexed_at) VALUES(?,?,?,?,?)
		ON CONFLICT(session_uid) DO UPDATE SET gen=excluded.gen,head=excluded.head,events=excluded.events,indexed_at=excluded.indexed_at`,
		s.UID, gen, head, pos, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return deleted, nil
}

// writeRows and writeBytes bound one batch of indexSession's writes.
// FTS5 flushes the terms it holds in memory to a new segment whenever
// a statement opens a savepoint, which every write to docs does because
// its triggers write fts, and each flush can start an automerge. A
// statement per row made a segment per row, and merging those was most
// of the cost of indexing (TKT-01M3MD3C). A statement per batch flushes
// a segment per batch.
const (
	writeRows  = 200
	writeBytes = 4 << 20
)

// docWriter batches the rows indexSession deletes and inserts into one
// statement each per writeRows rows or writeBytes of content.
type docWriter struct {
	tx    *sql.Tx
	del   []any
	ins   []any
	rows  int
	bytes int
}

func (w *docWriter) insert(ctx context.Context, row *docRow, sig int64) error {
	var content any
	if row.hasContent {
		content = row.content
	}
	w.ins = append(w.ins, row.uid, row.pos, sig, row.harness, row.project, row.eventType, row.actor, row.tool, row.toolError, row.raw, row.recorded, content)
	w.rows++
	w.bytes += len(row.content)
	if w.rows >= writeRows || w.bytes >= writeBytes {
		return w.flush(ctx)
	}
	return nil
}

// flush writes what w holds: its deletes, then its inserts.
func (w *docWriter) flush(ctx context.Context) error {
	if len(w.del) > 0 {
		if _, err := w.tx.ExecContext(ctx, `DELETE FROM docs WHERE id IN (?`+strings.Repeat(",?", len(w.del)-1)+`)`, w.del...); err != nil {
			return err
		}
	}
	if w.rows > 0 {
		const values = "(?,?,?,?,?,?,?,?,?,?,?,?)"
		if _, err := w.tx.ExecContext(ctx, `INSERT INTO docs(session_uid,pos,sig,harness,project_id,event_type,actor,tool_name,tool_error,raw_type,recorded_ns,content) VALUES`+values+strings.Repeat(","+values, w.rows-1), w.ins...); err != nil {
			return err
		}
	}
	clear(w.ins)
	w.del, w.ins, w.rows, w.bytes = w.del[:0], w.ins[:0], 0, 0
	return nil
}

type rowSig struct {
	id, sig int64
}

// rowSigs is uid's rows by position.
func (x *Index) rowSigs(ctx context.Context, uid string) (map[int64]rowSig, error) {
	rows, err := x.db.QueryContext(ctx, `SELECT pos,id,sig FROM docs WHERE session_uid=?`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]rowSig{}
	for rows.Next() {
		var pos int64
		var r rowSig
		if err := rows.Scan(&pos, &r.id, &r.sig); err != nil {
			return nil, err
		}
		out[pos] = r
	}
	return out, rows.Err()
}

// reclaim merges up to mergePages of the full-text index, forced when
// rows were deleted, and returns the pages that frees to the
// filesystem. more is true when the merge did work, so there may be
// more to merge, and when reclaim failed, so the next pass tries again.
// FTS5 documents a merge that did work as raising total_changes() by
// two or more on its connection.
func (x *Index) reclaim(ctx context.Context, forced bool) (more bool, err error) {
	conn, err := x.db.Conn(ctx)
	if err != nil {
		return true, err
	}
	defer conn.Close()
	var before, after int64
	if err := conn.QueryRowContext(ctx, `SELECT total_changes()`).Scan(&before); err != nil {
		return true, err
	}
	rank := mergePages
	if forced {
		rank = -mergePages
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO fts(fts, rank) VALUES('merge', ?)`, rank); err != nil {
		return true, err
	}
	if err := conn.QueryRowContext(ctx, `SELECT total_changes()`).Scan(&after); err != nil {
		return true, err
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA incremental_vacuum`); err != nil {
		return true, err
	}
	return after-before >= 2, nil
}

// remove drops every row of uid.
func (x *Index) remove(ctx context.Context, uid string) error {
	if _, err := x.db.ExecContext(ctx, `DELETE FROM indexed WHERE session_uid=?`, uid); err != nil {
		return err
	}
	return x.deleteDocs(ctx, uid, "1=?", 1)
}

// deleteDocs removes uid's rows matching cond in bounded transactions,
// so a large session does not hold the write lock for long.
func (x *Index) deleteDocs(ctx context.Context, uid, cond string, arg any) error {
	for {
		res, err := x.db.ExecContext(ctx, `DELETE FROM docs WHERE id IN (SELECT id FROM docs WHERE session_uid=? AND `+cond+` LIMIT 2000)`, uid, arg)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil || n == 0 {
			return err
		}
	}
}

type docRow struct {
	uid, harness, project string
	pos                   int64
	eventType, actor, raw string
	tool                  *string
	toolError             *bool
	recorded              *int64
	content               string
	hasContent            bool
}

// fill reads the fields search needs from one JSONL line. A line that
// is oversized or not JSON still gets a row, with no content, so
// positions stay dense.
func (d *docRow) fill(line []byte) {
	if line == nil {
		d.eventType = "unreadable"
		return
	}
	var ev normalize.Event
	if json.Unmarshal(line, &ev) != nil {
		d.eventType = "unreadable"
		return
	}
	d.eventType, d.actor, d.raw = ev.EventType, ev.Actor, ev.RawType
	d.tool, d.toolError = ev.Tool.Name, ev.Tool.IsError
	// Projection writes the projection time as recorded_at when the
	// source line had none, the same instant as ingested_at. It is
	// not the event's time, and it changes at every generation, so
	// such a row would be rewritten at every sync (TKT-01M3NENNN8).
	// A time the harness wrote cannot equal it: ingested_at is the
	// projection's clock to the nanosecond, read after the line was
	// written and uploaded.
	if ev.RecordedAt != "" && ev.RecordedAt != ev.IngestedAt {
		if t, err := time.Parse(time.RFC3339Nano, ev.RecordedAt); err == nil {
			ns := t.UnixNano()
			d.recorded = &ns
		}
	}
	if ev.ContentText != nil {
		d.content, d.hasContent = truncate(*ev.ContentText, IndexContentMax), true
	}
}

// sig is a signature of every field of d that the index stores, so a
// row at the same position in a new generation is kept when it is
// equal. The first eight bytes of a sha256 over the fields, each
// length-prefixed, so no two different rows share it in practice.
func (d *docRow) sig() int64 {
	h := sha256.New()
	put := func(present bool, s string) {
		var n [9]byte
		if present {
			n[0] = 1
		}
		binary.BigEndian.PutUint64(n[1:], uint64(len(s)))
		h.Write(n[:])
		h.Write([]byte(s))
	}
	put(true, d.harness)
	put(true, d.project)
	put(true, d.eventType)
	put(true, d.actor)
	put(true, d.raw)
	put(d.tool != nil, deref(d.tool))
	put(d.toolError != nil, fmt.Sprint(d.toolError != nil && *d.toolError))
	var rec string
	if d.recorded != nil {
		rec = fmt.Sprint(*d.recorded)
	}
	put(d.recorded != nil, rec)
	put(d.hasContent, d.content)
	return int64(binary.BigEndian.Uint64(h.Sum(nil)))
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// RemoveFromIndex drops uid from the index at path, for purge. A lake
// with no index has nothing to drop. The caller holds lake.lock.
func RemoveFromIndex(ctx context.Context, path, uid string) error {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	db, err := openIndexDB(path)
	if err != nil {
		return err
	}
	defer db.Close()
	x := &Index{db: db}
	return x.remove(ctx, uid)
}

// waitPasses blocks until n passes have finished, for tests.
func (x *Index) waitPasses(n int) {
	x.mu.Lock()
	defer x.mu.Unlock()
	for x.passes < n {
		x.passed.Wait()
	}
}
