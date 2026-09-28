package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/storage"
)

// Route classes for request counters. A class groups the paths one
// handler serves, so a label never carries a digest or a session id.
const (
	routeHealth    = "healthz"
	routeKeys      = "keys"
	routeStats     = "stats"
	routeConflicts = "conflicts"
	routeHello     = "hello"
	routeConfig    = "agent_config"
	routeRegister  = "register"
	routeCheck     = "blob_check"
	routePut       = "blob_put"
	routeManifest  = "manifest"
	routeWeb       = "web"
	routeOther     = "other"
)

func routeClass(r *http.Request) string {
	p := r.URL.Path
	switch {
	case p == "/healthz":
		return routeHealth
	case strings.HasPrefix(p, wellKnownPrefix):
		return routeKeys
	case p == "/v1/stats":
		return routeStats
	case p == "/v1/conflicts":
		return routeConflicts
	case p == "/v1/hello":
		return routeHello
	case strings.HasPrefix(p, "/v1/agent"):
		return routeConfig
	case strings.HasPrefix(p, "/v1/register"):
		return routeRegister
	case p == "/v1/blobs/check":
		return routeCheck
	case strings.HasPrefix(p, "/v1/blobs/"):
		return routePut
	case p == "/v1/manifests":
		return routeManifest
	case p == "/v1" || strings.HasPrefix(p, "/v1/"):
		return routeOther
	}
	return routeWeb
}

// requestCounts counts finished requests by route class and status
// class, and the request body bytes each class read.
type requestCounts struct {
	mu    sync.Mutex
	n     map[[2]string]int64
	bytes map[string]int64
}

func (c *requestCounts) add(route string, status int, body int64) {
	class := strconv.Itoa(status/100) + "xx"
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.n == nil {
		c.n, c.bytes = map[[2]string]int64{}, map[string]int64{}
	}
	c.n[[2]string{route, class}]++
	c.bytes[route] += body
}

func (c *requestCounts) snapshot() (map[[2]string]int64, map[string]int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := make(map[[2]string]int64, len(c.n))
	for k, v := range c.n {
		n[k] = v
	}
	b := make(map[string]int64, len(c.bytes))
	for k, v := range c.bytes {
		b[k] = v
	}
	return n, b
}

// MetricsInfo is what the metrics handler reads from serve rather than
// the lake.
type MetricsInfo struct {
	Version string
	Started time.Time
}

// MetricsHandler serves the lake's operating figures in the Prometheus
// text exposition format. It has no authentication: serve mounts it on
// its own listener, loopback unless the operator says otherwise. Each
// scrape reads the catalog, so scrape it every 15 seconds or slower.
func (s *Server) MetricsHandler(info MetricsInfo) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		var b strings.Builder
		if err := s.writeMetrics(ctx, &b, info); err != nil {
			s.logger().Error("metrics", "err", err.Error())
			http.Error(w, "metrics unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = io.WriteString(w, b.String())
	})
}

type metricWriter struct {
	w    io.Writer
	seen map[string]bool
}

// gauge writes one sample, with its HELP and TYPE the first time the
// name appears. labels alternate name and value.
func (m *metricWriter) sample(kind, name, help string, v float64, labels ...string) {
	if !m.seen[name] {
		m.seen[name] = true
		fmt.Fprintf(m.w, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, kind)
	}
	fmt.Fprint(m.w, name)
	if len(labels) > 0 {
		fmt.Fprint(m.w, "{")
		for i := 0; i+1 < len(labels); i += 2 {
			if i > 0 {
				fmt.Fprint(m.w, ",")
			}
			fmt.Fprintf(m.w, "%s=\"%s\"", labels[i], labelValue(labels[i+1]))
		}
		fmt.Fprint(m.w, "}")
	}
	fmt.Fprintf(m.w, " %s\n", strconv.FormatFloat(v, 'g', -1, 64))
}

// labelEscaper applies the text format's label escapes: backslash,
// double quote and newline. Everything else, tabs and non-ASCII
// included, is written as it is; Go's %q escapes would not parse.
var labelEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

func labelValue(s string) string { return labelEscaper.Replace(s) }

func unix(t time.Time) float64 { return float64(t.UnixNano()) / 1e9 }

func (s *Server) writeMetrics(ctx context.Context, w io.Writer, info MetricsInfo) error {
	m := &metricWriter{w: w, seen: map[string]bool{}}
	m.sample("gauge", "lampi_build_info", "The running terva-lampi build; always 1.", 1, "version", info.Version)
	if !info.Started.IsZero() {
		m.sample("gauge", "lampi_start_time_seconds", "When this serve process started, in Unix seconds.", unix(info.Started))
	}
	schema, err := s.Catalog.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	m.sample("gauge", "lampi_catalog_schema_version", "The catalog's schema version.", float64(schema))

	if latest, ok, err := s.Catalog.LatestStorage(ctx); err != nil {
		return err
	} else if ok {
		m.sample("gauge", "lampi_storage_sample_timestamp_seconds", "When the newest storage sample was taken, in Unix seconds.", unix(latest.At))
		for _, c := range storage.Components {
			u := latest.Measures[c]
			m.sample("gauge", "lampi_storage_bytes", "Disk use of a part of the lake directory at the newest sample.", float64(u.Bytes), "component", c)
		}
		for _, c := range storage.Components {
			u := latest.Measures[c]
			m.sample("gauge", "lampi_storage_files", "Regular files in a part of the lake directory at the newest sample.", float64(u.Files), "component", c)
		}
		if u, ok := latest.Measures[catalog.MeasureFSTotal]; ok {
			m.sample("gauge", "lampi_filesystem_size_bytes", "Size of the filesystem holding the lake directory.", float64(u.Bytes))
		}
		if u, ok := latest.Measures[catalog.MeasureFSFree]; ok {
			m.sample("gauge", "lampi_filesystem_free_bytes", "Space serve can still write on the lake's filesystem.", float64(u.Bytes))
		}
		if u, ok := latest.Measures[catalog.MeasureReferenced]; ok {
			m.sample("gauge", "lampi_artifact_referenced_bytes", "Logical bytes named by every artifact row.", float64(u.Bytes))
		}
		if u, ok := latest.Measures[catalog.MeasureUnique]; ok {
			m.sample("gauge", "lampi_artifact_unique_bytes", "Logical bytes of each distinct artifact digest counted once.", float64(u.Bytes))
		}
	}

	overview, err := s.Catalog.DashboardOverview(ctx)
	if err != nil {
		return err
	}
	m.sample("gauge", "lampi_sessions", "Stored sessions.", float64(overview.Sessions))
	for _, st := range []string{"pending", "failed", "ready", "unknown"} {
		m.sample("gauge", "lampi_sessions_by_normalization", "Stored sessions by normalization state.", float64(overview.Normalization[st]), "state", st)
	}
	pending, err := s.Catalog.PendingAudit(ctx)
	if err != nil {
		return err
	}
	m.sample("gauge", "lampi_audit_outbox_events", "Audit events committed to the catalog and not yet in audit.jsonl.", float64(pending))

	devices, err := s.Catalog.Devices(ctx)
	if err != nil {
		return err
	}
	activity, err := s.Catalog.MachinesActivity(ctx, s.now().Add(-24*time.Hour))
	if err != nil {
		return err
	}
	byMachine := map[string]catalog.MachineActivity{}
	for _, a := range activity {
		byMachine[a.MachineID] = a
	}
	contacts := s.Contacts()
	sort.Slice(devices, func(i, j int) bool { return devices[i].Name < devices[j].Name })
	for _, d := range devices {
		if d.State() == "revoked" {
			continue
		}
		if t, ok := contacts[d.ID]; ok {
			m.sample("gauge", "lampi_device_last_contact_timestamp_seconds", "A device's last authenticated request since serve started, in Unix seconds.", unix(t), "device", d.Name)
		}
	}
	for _, d := range devices {
		if d.State() == "revoked" || d.MachineID == "" {
			continue
		}
		a := byMachine[d.MachineID]
		last := a.LastUpload
		if a.LastUpdate.After(last) {
			last = a.LastUpdate
		}
		if !last.IsZero() {
			m.sample("gauge", "lampi_device_last_data_timestamp_seconds", "The newest upload the lake had not seen before from a device, in Unix seconds.", unix(last), "device", d.Name)
		}
	}

	n, bytes := s.requests.snapshot()
	keys := make([][2]string, 0, len(n))
	for k := range n {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i][0]+keys[i][1] < keys[j][0]+keys[j][1] })
	for _, k := range keys {
		m.sample("counter", "lampi_http_requests_total", "Finished requests since serve started, by route and status class.", float64(n[k]), "route", k[0], "code", k[1])
	}
	routes := make([]string, 0, len(bytes))
	for r := range bytes {
		routes = append(routes, r)
	}
	sort.Strings(routes)
	for _, r := range routes {
		m.sample("counter", "lampi_http_request_body_bytes_total", "Request body bytes read since serve started, by route.", float64(bytes[r]), "route", r)
	}
	return nil
}
