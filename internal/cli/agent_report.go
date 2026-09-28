package cli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
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
// no sync has finished since the last report. An idle agent learns of
// a profile edit from the answer to its report, so this bounds how long
// the edit takes to reach it.
var reportEvery = time.Minute

// reportTimeout bounds one report, so a lake that hangs does not hold
// the next one back.
const reportTimeout = 30 * time.Second

// syncOutcome is what the runner's last sync did. A sync that finished,
// refusals included, replaces last and clears the error. A sync that
// failed keeps the last finished one and records its error. inventory
// is the newest sync's projects, from any sync that read the harness
// homes, failed or not; nil until one has.
type syncOutcome struct {
	mu        sync.Mutex
	last      *protocol.AgentSyncReport
	err       string
	errAt     time.Time
	inventory []upload.InventoryRow
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
	if res.Inventory != nil {
		s.inventory = res.Inventory
	}
	s.mu.Unlock()
	select {
	case r.reported <- struct{}{}:
	default:
	}
}

// agentVersion is the running release for the report. The raw version
// variable is 0.0.0 in a goreleaser build, which links nothing in.
func agentVersion() string {
	v, _ := releaseVersion()
	return v
}

// report is the heartbeat for this lake now. The profile is read from
// the verified cache, which is the copy the rules in force came from.
func (r *lakeRunner) report() protocol.AgentReport {
	l := r.lake
	rep := protocol.AgentReport{
		AgentVersion: agentVersion(),
		MachineID:    l.opt.MachineID,
		AllowSource:  l.cfg.AllowFrom,
		DenySource:   l.cfg.DenyFrom(),
		Inventory:    l.inventory,
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

// pendingInventory is the inventory to send in the runner's mode, and
// its hash; false when no sync has read the harness homes yet. The
// hash leaves out when it was generated, so an unchanged machine
// hashes the same every time.
func (r *lakeRunner) pendingInventory(now time.Time) (protocol.AgentInventory, [sha256.Size]byte, bool) {
	s := &r.synced
	s.mu.Lock()
	rows := s.inventory
	s.mu.Unlock()
	if rows == nil {
		return protocol.AgentInventory{}, [sha256.Size]byte{}, false
	}
	inv := upload.Inventory(r.lake.inventory, rows, time.Time{})
	raw, _ := json.Marshal(inv)
	inv.GeneratedAt = now.UTC()
	return inv, sha256.Sum256(raw), true
}

// inventorySender posts the inventory when it differs from the last
// one the lake kept. A lake from before inventories answers 404; that
// is said once, and the same inventory is not sent to it again, since
// it can run to megabytes.
type inventorySender struct {
	sent    [sha256.Size]byte
	failing bool
}

func (is *inventorySender) send(ctx context.Context, r *lakeRunner) {
	inv, sum, ok := r.pendingInventory(time.Now())
	if !ok || sum == is.sent {
		return
	}
	ictx, cancel := context.WithTimeout(ctx, reportTimeout)
	defer cancel()
	resp, err := upload.PostInventory(ictx, r.lake.opt, inv)
	var se *upload.StatusError
	switch {
	case ctx.Err() != nil:
	case errors.As(err, &se) && se.Code == http.StatusNotFound:
		if !is.failing {
			r.errf("inventory: the lake does not take inventories; upgrade it to see this machine's projects there")
		}
		is.sent, is.failing = sum, true
	case err != nil:
		if !is.failing {
			r.errf("inventory: %v", err)
		}
		is.failing = true
	default:
		// A snapshot the lake did not keep lost to one it holds from
		// later, which only a clock set back makes; send again next time.
		if resp.Kept {
			is.sent = sum
		}
		is.failing = false
	}
}

// watchReports sends a report once the runner may push, after each
// sync, and every interval in between, until ctx ends, and the
// inventory after a report when it has changed. A failure is said once
// per run of failures; a lake from before reports answers 404 on every
// try and is said once.
func watchReports(ctx context.Context, r *lakeRunner, interval time.Duration) {
	select {
	case <-ctx.Done():
		return
	case <-r.ready:
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	failing := false
	var inventory inventorySender
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
		inventory.send(ctx, r)
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
