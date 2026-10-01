package recall

import (
	"context"
	"errors"
	"io"
)

// SelectStats says what one Select read.
type SelectStats struct {
	// Rows is the lines written, and Sessions the sessions read.
	Rows     int64 `json:"rows"`
	Sessions int   `json:"sessions"`
	// Skipped counts sessions left out because their publication
	// changed or went away while they were opened. Oversized counts
	// matching lines too long to read, written only when Fields is set,
	// with every field null.
	Skipped   int   `json:"skipped"`
	Oversized int64 `json:"oversized"`
}

// Select writes, through emit, every event of every published session
// that reaches allows and f keeps, one line each, without the newline:
// the event as stored, or the object Fields.Project makes when fields
// is set. It reads each session from one pinned generation, as Events
// does, so a session's lines never mix generations. f and fields must
// be valid. Select stops at the first error from reaches, emit or a
// read, and returns what it had written.
func (r *Reader) Select(ctx context.Context, f EventFilter, fields Fields, reaches func(uid string) (bool, error), emit func(line []byte) error) (SelectStats, error) {
	var st SelectStats
	sessions, err := r.catalog.PublishedSessions(ctx)
	if err != nil {
		return st, err
	}
	for _, s := range sessions {
		if s.State != "ready" || !f.Session(s.Harness, s.ProjectID) {
			continue
		}
		ok, err := reaches(s.UID)
		if err != nil {
			return st, err
		}
		if !ok {
			continue
		}
		snap, err := r.open(ctx, s.UID)
		var gone UnavailableError
		if errors.As(err, &gone) || errors.Is(err, ErrGenerationChanged) || errors.Is(err, ErrNotFound) {
			st.Skipped++
			continue
		}
		if err != nil {
			return st, err
		}
		err = r.selectSession(ctx, snap, f, fields, emit, &st)
		snap.Close()
		if err != nil {
			return st, err
		}
		st.Sessions++
	}
	return st, nil
}

func (r *Reader) selectSession(ctx context.Context, snap *snapshot, f EventFilter, fields Fields, emit func([]byte) error, st *SelectStats) error {
	br, _, _, err := snap.lines(ctx, 0, 0)
	if err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, n, rerr := readLine(br, MaxLine)
		if rerr == io.EOF && n == 0 {
			return nil
		}
		if rerr != nil && rerr != io.EOF {
			return rerr
		}
		if f.Line(line) {
			out := line
			switch {
			case fields != nil:
				out = fields.Project(line)
				if line == nil {
					st.Oversized++
				}
			case line == nil:
				// The line was not kept, so there is nothing to write.
				st.Oversized++
				continue
			}
			if err := emit(out); err != nil {
				return err
			}
			st.Rows++
		}
		if rerr == io.EOF {
			return nil
		}
	}
}
