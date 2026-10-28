package recall

import (
	"context"
	"io"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/diagnose"
)

// Diagnose reads uid's published events once, from one pinned
// generation, and returns the diagnosis under the shipped rules. A
// session outside scope is ErrNotFound; one with no readable output is
// UnavailableError. The report carries counts, sizes and positions,
// never content, so a route can serialize it as it is and link each
// position with EventLink.
func (r *Reader) Diagnose(ctx context.Context, scope catalog.Scope, uid string) (diagnose.Report, error) {
	if err := r.inScope(ctx, scope, uid); err != nil {
		return diagnose.Report{}, err
	}
	snap, err := r.open(ctx, uid)
	if err != nil {
		return diagnose.Report{}, err
	}
	defer snap.Close()
	br, _, _, err := snap.lines(ctx, 0, 0)
	if err != nil {
		return diagnose.Report{}, err
	}
	a := diagnose.New(nil)
	for n := 0; ; n++ {
		if n%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return diagnose.Report{}, err
			}
		}
		line, read, rerr := readLine(br, MaxLine)
		if rerr == io.EOF && read == 0 {
			break
		}
		if rerr != nil && rerr != io.EOF {
			return diagnose.Report{}, rerr
		}
		a.Line(line)
		if rerr == io.EOF {
			break
		}
	}
	rep := a.Report()
	rep.SessionUID, rep.Generation, rep.Head = uid, snap.pub.Gen, snap.pub.Head
	return rep, nil
}
