package catalog

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// migrateStorageSamples adds storage_samples: what the lake directory
// used at each sample, one row per measure. A measure is a component of
// the lake directory (storage.Components), the filesystem's capacity,
// or the bytes the catalog references. Like head_updates it is
// prospective; nothing is reconstructed for the time before it.
func migrateStorageSamples(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE storage_samples (
		sampled_ns INTEGER NOT NULL,
		measure TEXT NOT NULL,
		bytes INTEGER NOT NULL,
		files INTEGER NOT NULL,
		PRIMARY KEY (sampled_ns, measure)
	) WITHOUT ROWID`)
	return err
}

// Measures recorded beside the lake directory's components. Files is
// zero for the filesystem measures and the count of rows or digests for
// the referenced ones.
const (
	// MeasureFSTotal and MeasureFSFree are the filesystem holding the
	// lake directory. Free is what serve itself could still write.
	MeasureFSTotal = "fs.total"
	MeasureFSFree  = "fs.free"
	// MeasureReferenced sums the size of every artifact row: the bytes
	// the lake would hold if it kept a copy per row.
	MeasureReferenced = "artifacts.referenced"
	// MeasureUnique sums the size of each distinct digest once: the
	// logical bytes the CAS holds.
	MeasureUnique = "artifacts.unique"
	// MeasureCurrent sums the size of each path's current version: the
	// raw bytes the machines that uploaded them hold now. It is what
	// the stored blobs' disk use is compared with.
	MeasureCurrent = "artifacts.current"
	// MeasureCurrentUnique sums each distinct digest among the current
	// versions once. What it falls short of MeasureCurrent by is files
	// that are byte-for-byte copies of another, stored once.
	MeasureCurrentUnique = "artifacts.current_unique"
)

// StorageUse is one measure in a sample.
type StorageUse struct {
	Bytes int64 `json:"bytes"`
	Files int64 `json:"files"`
}

// StorageSample is every measure taken at one time.
type StorageSample struct {
	At       time.Time             `json:"at"`
	Measures map[string]StorageUse `json:"measures"`
}

// storageDetail is how long every sample is kept. Older samples are
// thinned to the last one of each UTC day, so a year of history is a
// few hundred samples.
const storageDetail = 14 * 24 * time.Hour

// RecordStorage stores a sample and thins samples older than
// storageDetail before it.
func (c *Catalog) RecordStorage(ctx context.Context, s StorageSample) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	at := s.At.UnixNano()
	for name, u := range s.Measures {
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO storage_samples(sampled_ns, measure, bytes, files) VALUES(?, ?, ?, ?)`, at, name, u.Bytes, u.Files); err != nil {
			return fmt.Errorf("catalog: %w", err)
		}
	}
	cutoff := s.At.Add(-storageDetail).UnixNano()
	const day = int64(24 * time.Hour)
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM storage_samples
		WHERE sampled_ns < ?1 AND sampled_ns NOT IN (
			SELECT MAX(sampled_ns) FROM storage_samples
			WHERE sampled_ns < ?1
			GROUP BY sampled_ns / ?2
		)`, cutoff, day); err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	return nil
}

// StorageSamples returns the samples taken in [from, to), oldest first.
func (c *Catalog) StorageSamples(ctx context.Context, from, to time.Time) ([]StorageSample, error) {
	return c.storageSamples(ctx, `
		SELECT sampled_ns, measure, bytes, files FROM storage_samples
		WHERE sampled_ns >= ? AND sampled_ns < ?
		ORDER BY sampled_ns, measure`, from.UnixNano(), to.UnixNano())
}

// LatestStorage returns the newest sample. ok is false when there is
// none.
func (c *Catalog) LatestStorage(ctx context.Context) (s StorageSample, ok bool, err error) {
	got, err := c.storageSamples(ctx, `
		SELECT sampled_ns, measure, bytes, files FROM storage_samples
		WHERE sampled_ns = (SELECT MAX(sampled_ns) FROM storage_samples)
		ORDER BY measure`)
	if err != nil || len(got) == 0 {
		return StorageSample{}, false, err
	}
	return got[0], true, nil
}

func (c *Catalog) storageSamples(ctx context.Context, query string, args ...any) ([]StorageSample, error) {
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	defer rows.Close()
	var out []StorageSample
	for rows.Next() {
		var at int64
		var name string
		var u StorageUse
		if err := rows.Scan(&at, &name, &u.Bytes, &u.Files); err != nil {
			return nil, fmt.Errorf("catalog: %w", err)
		}
		if n := len(out); n == 0 || out[n-1].At.UnixNano() != at {
			out = append(out, StorageSample{At: time.Unix(0, at).UTC(), Measures: map[string]StorageUse{}})
		}
		out[len(out)-1].Measures[name] = u
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	return out, nil
}

// ArtifactUse is the logical bytes the catalog references, measured
// four ways. Versions of a growing file are rows of their own, so
// Referenced and Unique count every continuation in full; Current and
// CurrentUnique count only the newest version of each path.
type ArtifactUse struct {
	Referenced, Unique, Current, CurrentUnique StorageUse
}

// ArtifactBytes measures the logical bytes the catalog references:
// every artifact row, each distinct digest once, and the same two for
// the current rows only. All four come from one read transaction, so
// an upload committed between them cannot leave a unique count above
// its total.
func (c *Catalog) ArtifactBytes(ctx context.Context) (ArtifactUse, error) {
	var u ArtifactUse
	tx, err := c.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return u, fmt.Errorf("catalog: %w", err)
	}
	defer tx.Rollback()
	for _, q := range []struct {
		dst   *StorageUse
		query string
	}{
		{&u.Referenced, `SELECT COUNT(*), COALESCE(SUM(size), 0) FROM artifacts`},
		{&u.Unique, `SELECT COUNT(*), COALESCE(SUM(size), 0) FROM (SELECT MAX(size) AS size FROM artifacts GROUP BY sha256)`},
		{&u.Current, `SELECT COUNT(*), COALESCE(SUM(size), 0) FROM artifacts WHERE current = 1`},
		{&u.CurrentUnique, `SELECT COUNT(*), COALESCE(SUM(size), 0) FROM (SELECT MAX(size) AS size FROM artifacts WHERE current = 1 GROUP BY sha256)`},
	} {
		if err := tx.QueryRowContext(ctx, q.query).Scan(&q.dst.Files, &q.dst.Bytes); err != nil {
			return u, fmt.Errorf("catalog: %w", err)
		}
	}
	return u, nil
}
