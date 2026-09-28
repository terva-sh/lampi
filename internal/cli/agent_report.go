package cli

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/lakeprofile"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/upload"
)

// reportEvery is how often a running agent reports to each lake when
// no sync has finished since the last report.
var reportEvery = 5 * time.Minute

// reportTimeout bounds one report, so a lake that hangs does not hold
// the next one back.
const reportTimeout = 30 * time.Second

// syncOutcome is what the runner's last sync did. A sync that finished,
// refusals included, replaces last and clears the error. A sync that
// failed keeps the last finished one and records its error.
type syncOutcome struct {
	mu    sync.Mutex
	last  *protocol.AgentSyncReport
	err   string
	errAt time.Time
}

// noteSync records a sync's outcome and asks for a report.
func (r *lakeRunner) noteSync(res upload.Result, err error, now time.Time) {
	var rejected *upload.Rejected
	s := &r.synced
	s.mu.Lock()
	if err == nil || errors.As(err, &rejected) {
		s.last = &protocol.AgentSyncReport{
			At:          now.UTC(),
			Checked:     res.Checked,
			Missing:     res.Missing,
			Uploaded:    res.Uploaded,
			Manifests:   res.Manifests,
			Refused:     res.Refused,
			Quarantined: res.Quarantined,
			Unchanged:   res.Unchanged,
		}
		s.err, s.errAt = "", time.Time{}
	} else {
		s.err, s.errAt = err.Error(), now.UTC()
	}
	s.mu.Unlock()
	select {
	case r.reported <- struct{}{}:
	default:
	}
}

// report is the heartbeat for this lake now. The profile is read from
// the verified cache, which is the copy the rules in force came from.
func (r *lakeRunner) report() protocol.AgentReport {
	l := r.lake
	rep := protocol.AgentReport{
		AgentVersion: version,
		MachineID:    l.opt.MachineID,
		AllowSource:  l.cfg.AllowFrom,
		DenySource:   l.cfg.DenyFrom(),
	}
	if rep.AllowSource == "" {
		rep.AllowSource = config.OriginLocal
	}
	if lakeprofile.Pinned(l.cfg) {
		if d, ok, err := lakeprofile.Load(l.opt.LakeStateDir, l.cfg); err == nil && ok {
			rep.Profile, rep.ProfileVersion = d.Payload.Profile, d.Payload.Version
		}
	}
	s := &r.synced
	s.mu.Lock()
	if s.last != nil {
		last := *s.last
		rep.LastSync = &last
	}
	rep.LastError, rep.LastErrorAt = s.err, s.errAt
	s.mu.Unlock()
	return rep
}

// watchReports sends a report once the runner may push, after each
// sync, and every interval in between, until ctx ends. A failure is
// said once per run of failures; a lake from before reports answers
// 404 on every try and is said once.
func watchReports(ctx context.Context, r *lakeRunner, interval time.Duration) {
	select {
	case <-ctx.Done():
		return
	case <-r.ready:
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	failing := false
	send := func() {
		rctx, cancel := context.WithTimeout(ctx, reportTimeout)
		defer cancel()
		_, err := upload.PostReport(rctx, r.lake.opt, r.report())
		switch {
		case ctx.Err() != nil:
		case err != nil:
			if !failing {
				var se *upload.StatusError
				if errors.As(err, &se) && se.Code == http.StatusNotFound {
					r.errf("report: the lake does not take agent reports; upgrade it to see this machine's status there")
				} else {
					r.errf("report: %v", err)
				}
			}
			failing = true
		default:
			failing = false
		}
		t.Reset(interval)
	}
	send()
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.reported:
			send()
		case <-t.C:
			send()
		}
	}
}
