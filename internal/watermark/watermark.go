// Package watermark remembers how far each session file has been uploaded.
//
// The key is (machine_id, harness, root_path, relative_path). The record
// is the last uploaded size, mtime, content sha256, and the byte offset
// for an append-only file. Commit is the only write, and it refuses to
// store anything unless the server ACKed the manifest. Plan turns that
// record into the byte range a later upload should send: nothing, the
// tail, or the whole file after a truncate or a non-prefix rewrite.
package watermark

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"terva.sh/lampi/internal/protocol"

	_ "modernc.org/sqlite"
)

// ErrNotAcked is returned when Commit is asked to store a mark the
// server has not acknowledged.
var ErrNotAcked = errors.New("watermark: manifest was not acknowledged")

const (
	// KindNew is a path with no stored mark. Read from offset 0.
	KindNew = "new"
	// KindUnchanged means the stored bytes are still the file.
	KindUnchanged = "unchanged"
	// KindTail is a strict append. Offset is the stored cursor.
	KindTail = "tail"
	// KindReplace is a truncate or a rewrite that is not a prefix of the
	// stored bytes. Read from offset 0.
	KindReplace = "replace"
	// KindProbe means the file grew and the prefix hash was not supplied.
	// Hash file bytes [0:Offset] — not the whole file — and call Plan again.
	KindProbe = "probe"
)

// Mark is one file's upload cursor.
//
// SHA256 is the sha256 of the file bytes in [0:Offset] at commit time,
// except when Offset is past Size. That mark is a stale client: Size
// and SHA256 are the local file, and Offset is the longer lake head.
// When Offset equals Size, SHA256 is the hash of the whole file. Plan
// compares both a full-file hash and a prefix hash against this value;
// they are the same hash only for that snapshot.
type Mark struct {
	MachineID string
	Harness   string
	Root      string
	RelPath   string
	Size      int64
	ModTime   time.Time
	SHA256    string
	Offset    int64
}

// Stat is the file as it sits on disk now. FullSHA and PrefixSHA are
// hex sha256. Leave a hash empty when it has not been computed.
// PrefixSHA is the hash of bytes [0:mark.Offset].
type Stat struct {
	Size      int64
	ModTime   time.Time
	FullSHA   string
	PrefixSHA string
}

// Decision is the slice Plan wants uploaded.
// For KindTail, read [Offset, Offset+Length). For KindProbe, do not
// upload yet: hash the first Offset bytes and call Plan again. Length
// on a probe is the prospective tail, not a license to send it.
type Decision struct {
	Offset int64
	Length int64
	Kind   string
}

// Store persists marks.
type Store interface {
	Get(ctx context.Context, key Mark) (Mark, bool, error)
	Commit(ctx context.Context, mark Mark, ack protocol.ManifestAck) error
}

// DB is one SQLite file.
type DB struct {
	db   *sql.DB
	path string
}

var _ Store = (*DB)(nil)

// File is the watermark path inside a lampi state directory.
func File(stateDir string) string {
	return filepath.Join(stateDir, "watermarks.db")
}

// Open creates the store. The file is owner-read. Re-opening the same
// path keeps the cursors.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("watermark: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("watermark: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		db.Close()
		return nil, fmt.Errorf("watermark: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		db.Close()
		return nil, fmt.Errorf("watermark: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("watermark: %w", err)
	}
	// The parent directory is 0700. The database and its WAL sidecars
	// are 0600. Sidecars appear when WAL mode is turned on.
	if err := chmodPrivate(path); err != nil {
		db.Close()
		return nil, fmt.Errorf("watermark: %w", err)
	}
	return &DB{db: db, path: path}, nil
}

const schema = `
CREATE TABLE IF NOT EXISTS watermarks (
    machine_id TEXT NOT NULL,
    harness TEXT NOT NULL,
    root_path TEXT NOT NULL,
    relative_path TEXT NOT NULL,
    size INTEGER NOT NULL,
    mtime_unix_nano INTEGER NOT NULL,
    sha256 TEXT NOT NULL,
    byte_offset INTEGER NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (machine_id, harness, root_path, relative_path)
);
`

// Close releases the database.
func (s *DB) Close() error {
	return s.db.Close()
}

// Summary is the operator view of the store: how many paths are marked,
// how many bytes those marks cover, and when the newest one was committed.
type Summary struct {
	Paths  int
	Bytes  int64
	Newest time.Time
}

// Summary reads the store. An empty database is a zero Summary.
func (s *DB) Summary(ctx context.Context) (Summary, error) {
	var sum Summary
	var newest sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(size), 0), MAX(updated_at) FROM watermarks`,
	).Scan(&sum.Paths, &sum.Bytes, &newest)
	if err != nil {
		return Summary{}, fmt.Errorf("watermark: %w", err)
	}
	if newest.Valid && newest.String != "" {
		t, err := time.Parse(time.RFC3339Nano, newest.String)
		if err != nil {
			return Summary{}, fmt.Errorf("watermark: updated_at: %w", err)
		}
		sum.Newest = t.UTC()
	}
	return sum, nil
}

// Get returns the mark for key. The size, mtime, sha256, and offset on
// key are ignored. ok is false when this path has never been ACKed.
func (s *DB) Get(ctx context.Context, key Mark) (Mark, bool, error) {
	var m Mark
	var nano int64
	err := s.db.QueryRowContext(ctx, `
		SELECT machine_id, harness, root_path, relative_path, size, mtime_unix_nano, sha256, byte_offset
		FROM watermarks
		WHERE machine_id = ? AND harness = ? AND root_path = ? AND relative_path = ?`,
		key.MachineID, key.Harness, key.Root, key.RelPath,
	).Scan(&m.MachineID, &m.Harness, &m.Root, &m.RelPath, &m.Size, &nano, &m.SHA256, &m.Offset)
	if errors.Is(err, sql.ErrNoRows) {
		return Mark{}, false, nil
	}
	if err != nil {
		return Mark{}, false, fmt.Errorf("watermark: %w", err)
	}
	m.ModTime = time.Unix(0, nano).UTC()
	return m, true, nil
}

// Commit stores mark after the server ACKs the manifest. An empty
// SessionUID is not an ACK: the row is left as it was and ErrNotAcked
// is returned.
func (s *DB) Commit(ctx context.Context, mark Mark, ack protocol.ManifestAck) error {
	if ack.SessionUID == "" {
		return ErrNotAcked
	}
	if mark.MachineID == "" || mark.Harness == "" || mark.Root == "" || mark.RelPath == "" {
		return fmt.Errorf("watermark: machine_id, harness, root_path, and relative_path are required")
	}
	if mark.Size < 0 || mark.Offset < 0 {
		return fmt.Errorf("watermark: negative size or offset")
	}
	// Offset may pass Size. A stale client stores the local file as
	// Size and SHA256, and the longer lake head as Offset. A commit of
	// a file this machine uploaded in full keeps Offset == Size.
	if !protocol.ValidDigest(mark.SHA256) {
		return fmt.Errorf("watermark: invalid sha256")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO watermarks (
			machine_id, harness, root_path, relative_path,
			size, mtime_unix_nano, sha256, byte_offset, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (machine_id, harness, root_path, relative_path) DO UPDATE SET
			size = excluded.size,
			mtime_unix_nano = excluded.mtime_unix_nano,
			sha256 = excluded.sha256,
			byte_offset = excluded.byte_offset,
			updated_at = excluded.updated_at`,
		mark.MachineID, mark.Harness, mark.Root, mark.RelPath,
		mark.Size, mark.ModTime.UTC().UnixNano(), mark.SHA256, mark.Offset, now,
	)
	if err != nil {
		return fmt.Errorf("watermark: %w", err)
	}
	if err := chmodPrivate(s.path); err != nil {
		return fmt.Errorf("watermark: %w", err)
	}
	return nil
}

// chmodPrivate keeps the database and its WAL sidecars owner-read.
// A sidecar that does not exist yet is not an error.
func chmodPrivate(path string) error {
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		err := os.Chmod(path+suffix, 0o600)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// Plan decides what a caller should upload for mark given the file now.
// A zero mark (Get returned false) is KindNew. A matching full hash, or
// the same size and mtime with no hash supplied, is KindUnchanged.
// Growth whose prefix hash equals mark.SHA256 is KindTail: the upload
// starts at Offset and does not resend the prefix. mark.SHA256 is the
// hash of bytes [0:Offset], so a full-file hash matches it when the
// file is still that snapshot (Offset == Size).
//
// A stale client is the exception: Offset is the lake head and is past
// Size, and SHA256 is the shorter local file. A full-hash match is
// still KindUnchanged. A file shorter than Offset when Offset == Size
// is a truncate, and that stays KindReplace.
func Plan(mark Mark, st Stat) Decision {
	if mark.SHA256 == "" && mark.Offset == 0 && mark.Size == 0 {
		return Decision{Offset: 0, Length: st.Size, Kind: KindNew}
	}
	if st.FullSHA != "" && st.FullSHA == mark.SHA256 {
		return Decision{Offset: mark.Offset, Length: 0, Kind: KindUnchanged}
	}
	if st.Size == mark.Size && st.ModTime.Equal(mark.ModTime) && st.FullSHA == "" && st.PrefixSHA == "" {
		return Decision{Offset: mark.Offset, Length: 0, Kind: KindUnchanged}
	}
	if st.Size > mark.Offset && st.PrefixSHA != "" && st.PrefixSHA == mark.SHA256 {
		return Decision{Offset: mark.Offset, Length: st.Size - mark.Offset, Kind: KindTail}
	}
	if st.Size > mark.Offset && st.PrefixSHA == "" && st.FullSHA == "" {
		return Decision{Offset: mark.Offset, Length: st.Size - mark.Offset, Kind: KindProbe}
	}
	return Decision{Offset: 0, Length: st.Size, Kind: KindReplace}
}

// HashPrefix is the sha256 of the first n bytes of r. It stops at n, so
// a caller checking a watermark does not hash the tail it is about to upload.
func HashPrefix(r io.Reader, n int64) (string, error) {
	if n < 0 {
		return "", fmt.Errorf("watermark: negative prefix")
	}
	h := sha256.New()
	if _, err := io.CopyN(h, r, n); err != nil {
		return "", fmt.Errorf("watermark: prefix: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
