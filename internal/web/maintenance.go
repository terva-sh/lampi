package web

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/webauth"
)

// Maintenance coordinates one asynchronous job at a time. Close cancels and
// joins it before the lake closes its stores. Run is supplied by serve.
type Maintenance struct {
	ctx    context.Context
	cancel context.CancelFunc
	run    func(context.Context, string) (string, error)
	mu     sync.Mutex
	wg     sync.WaitGroup
	closed bool
	state  maintenanceState
}

type maintenanceState struct {
	Action, Message string
	Running         bool
}

func NewMaintenance(parent context.Context, run func(context.Context, string) (string, error)) *Maintenance {
	ctx, cancel := context.WithCancel(parent)
	return &Maintenance{ctx: ctx, cancel: cancel, run: run}
}

func (m *Maintenance) Close() {
	m.mu.Lock()
	m.closed = true
	m.cancel()
	m.mu.Unlock()
	m.wg.Wait()
}

func (m *Maintenance) status() maintenanceState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

var errMaintenanceBusy = errors.New("maintenance already running or stopping")

func (m *Maintenance) start(ctx context.Context, action string, record func(context.Context, string, string) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.state.Running {
		return errMaintenanceBusy
	}
	if err := record(ctx, audit.MaintenanceRequested, action); err != nil {
		return err
	}
	m.state = maintenanceState{Action: action, Running: true, Message: "Maintenance is running. Results will appear here when it finishes."}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		jobCtx, cancel := context.WithTimeout(m.ctx, time.Hour)
		defer cancel()
		message, err := m.run(jobCtx, action)
		kind := audit.MaintenanceFinished
		if err != nil {
			kind = audit.MaintenanceFailed
			message = "Maintenance did not finish. Check the lake's logs before retrying."
		}
		// Give the completion record its own budget even on shutdown.
		auditCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
		if record(auditCtx, kind, action) != nil {
			message += " The result's audit record could not be saved."
		}
		done()
		m.mu.Lock()
		m.state = maintenanceState{Action: action, Message: message}
		m.mu.Unlock()
	}()
	return nil
}

var maintenanceActions = map[string]string{
	"search":  "Compact search index",
	"uploads": "Clean up old uploads",
	"sample":  "Refresh storage measurements",
}

func (s *Server) maintenanceRoutes(m *http.ServeMux) {
	if s.ops == nil || s.ops.Maintenance == nil || s.reg == nil {
		return
	}
	m.Handle("POST /operations/maintenance", s.auth.Guard(webauth.AdminOnly(http.HandlerFunc(s.requestMaintenance))))
}

func (s *Server) requestMaintenance(w http.ResponseWriter, r *http.Request) {
	if !s.readForm(w, r) {
		return
	}
	if !webauth.Fresh(r, s.now()) {
		http.Redirect(w, r, webauth.FreshLoginURL("/operations"), http.StatusSeeOther)
		return
	}
	action := r.PostForm.Get("action")
	if _, ok := maintenanceActions[action]; !ok || len(r.PostForm["action"]) != 1 || action == "search" && s.index == nil {
		http.Error(w, "Choose a maintenance action.", http.StatusBadRequest)
		return
	}
	ident, _ := webauth.Current(r)
	who := actor(ident).Audit
	lake := s.reg.Lake()
	record := func(ctx context.Context, kind, detail string) error {
		err := lake.Catalog.QueueAudit(ctx, s.now(), audit.Event{Kind: kind, Actor: who, Detail: detail})
		if err == nil {
			err = lake.Catalog.FlushAudit(ctx, lake.Dir)
		}
		if err != nil {
			s.logError(r, "recording maintenance failed", err)
		}
		return err
	}
	err := s.ops.Maintenance.start(r.Context(), action, record)
	if errors.Is(err, errMaintenanceBusy) {
		http.Error(w, "Maintenance is already running. Return to Operations to see its progress.", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, "The maintenance request could not be recorded.", http.StatusInternalServerError)
		return
	}
	rg, ok := findOpsRange(r.PostForm.Get("range"))
	if !ok {
		rg = opsRanges[0]
	}
	http.Redirect(w, r, "/operations?range="+rg.Key, http.StatusSeeOther)
}
