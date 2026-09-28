package upload

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"terva.sh/lampi/internal/protocol"
)

// Transport limits. None of them bounds a whole request, so a large PUT
// on a slow link still finishes. A connection that stops moving bytes
// is cut by the stall watchdog or by ResponseHeaderTimeout.
const (
	dialTimeout           = 30 * time.Second
	tlsHandshakeTimeout   = 15 * time.Second
	responseHeaderTimeout = 60 * time.Second
	idleConnTimeout       = 90 * time.Second
	// DefaultStallTimeout is how long a request may move no bytes before
	// it is cancelled. Options.StallTimeout replaces it.
	DefaultStallTimeout = 60 * time.Second
	// DefaultPieceBytes is the Content-Range slice sync and the agent use.
	DefaultPieceBytes = 4 << 20
	// checkBatch is the most digests one blobs/check carries. The lake
	// allows more; a smaller request is cheaper to repeat.
	checkBatch = 1000
)

// NewClient is the client Sync uses when Options.Client is nil. It has
// connect, TLS, and response-header timeouts and no Client.Timeout.
func NewClient() *http.Client {
	return &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ResponseHeaderTimeout: responseHeaderTimeout,
		IdleConnTimeout:       idleConnTimeout,
		ExpectContinueTimeout: time.Second,
		MaxIdleConnsPerHost:   2,
		ForceAttemptHTTP2:     true,
	}}
}

// defaultClient is shared so the agent reuses connections across syncs.
var defaultClient = NewClient()

// StatusError is a lake answer outside 2xx.
type StatusError struct {
	Method string
	Path   string
	Code   int
	Status string
	Body   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("upload: %s %s: %s: %s", e.Method, e.Path, e.Status, e.Body)
}

// Unauthorized reports whether err is the lake refusing the token.
func Unauthorized(err error) bool {
	var se *StatusError
	if !errors.As(err, &se) {
		return false
	}
	return se.Code == http.StatusUnauthorized || se.Code == http.StatusForbidden
}

// CheckToken refuses to send a bearer token in the clear. A token goes
// over https, or over http only to localhost, 127.0.0.0/8, or ::1.
func CheckToken(server, token string) error {
	if token == "" {
		return nil
	}
	u, err := url.Parse(server)
	if err != nil {
		return fmt.Errorf("upload: server URL: %w", err)
	}
	if u.Scheme != "http" || loopbackHost(u.Hostname()) {
		return nil
	}
	return fmt.Errorf("refusing to send the device token to %s over plain http; use an https URL, or http only to localhost, 127.0.0.0/8, or ::1", u.Host)
}

// loopbackHost reports whether host is localhost or a loopback IP. It
// does not resolve names.
func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

var errStalled = errors.New("stalled")

// stallWatch cancels a request that moves no bytes for d. It is paused
// between the last request byte and the response headers: bytes still
// in the socket buffer drain there, and ResponseHeaderTimeout owns
// that wait.
//
// The request body is read on the transport's goroutine and the
// response on the caller's, so the timer is only touched under mu.
type stallWatch struct {
	d  time.Duration
	mu sync.Mutex
	t  *time.Timer
}

func newStallWatch(d time.Duration, cancel context.CancelCauseFunc) *stallWatch {
	return &stallWatch{d: d, t: time.AfterFunc(d, func() { cancel(errStalled) })}
}

func (w *stallWatch) touch() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.t.Reset(w.d)
}

func (w *stallWatch) pause() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.t.Stop()
}

// progressBody is a request body that feeds the watchdog.
type progressBody struct {
	r    *bytes.Reader
	w    *stallWatch
	once sync.Once
}

func (p *progressBody) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if err != nil || p.r.Len() == 0 {
		p.once.Do(p.w.pause)
	} else if n > 0 {
		p.w.touch()
	}
	return n, err
}

func (p *progressBody) Close() error { return nil }

// progressReader is a response body that feeds the watchdog.
type progressReader struct {
	r io.Reader
	w *stallWatch
}

func (p progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.w.touch()
	}
	return n, err
}

// FetchAgentConfig gets the device's signed base configuration. It does
// not check the signature: the caller holds the pin.
func FetchAgentConfig(ctx context.Context, opt Options) (*protocol.Signed, error) {
	client := opt.Client
	if client == nil {
		client = NewClient()
	}
	var s protocol.Signed
	if err := doJSON(ctx, client, opt, http.MethodGet, protocol.AgentConfigPath, nil, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// PostReport sends the agent's heartbeat to the lake.
func PostReport(ctx context.Context, opt Options, rep protocol.AgentReport) (protocol.AgentReportResponse, error) {
	client := opt.Client
	if client == nil {
		client = NewClient()
	}
	raw, err := json.Marshal(rep)
	if err != nil {
		return protocol.AgentReportResponse{}, err
	}
	var resp protocol.AgentReportResponse
	if err := doJSON(ctx, client, opt, http.MethodPost, protocol.AgentReportPath, raw, &resp); err != nil {
		return protocol.AgentReportResponse{}, err
	}
	return resp, nil
}

// PostInventory sends the agent's inventory to the lake.
func PostInventory(ctx context.Context, opt Options, inv protocol.AgentInventory) (protocol.AgentInventoryResponse, error) {
	client := opt.Client
	if client == nil {
		client = NewClient()
	}
	raw, err := json.Marshal(inv)
	if err != nil {
		return protocol.AgentInventoryResponse{}, err
	}
	var resp protocol.AgentInventoryResponse
	if err := doJSON(ctx, client, opt, http.MethodPost, protocol.AgentInventoryPath, raw, &resp); err != nil {
		return protocol.AgentInventoryResponse{}, err
	}
	return resp, nil
}

// ErrNoKeyEndpoint is FetchKeys against a lake from before identities.
var ErrNoKeyEndpoint = errors.New("the lake has no key endpoint; upgrade the lake before registering agents with it")

// FetchKeys gets the lake's key list signed over nonce. It does not
// verify the signature: the caller knows which key it expects.
func FetchKeys(ctx context.Context, server, nonce string) (*protocol.Signed, protocol.KeysPayload, error) {
	opt := Options{ServerURL: server}
	var s protocol.Signed
	p := protocol.KeysPath + "?" + url.Values{"nonce": {nonce}}.Encode()
	if err := doJSON(ctx, NewClient(), opt, http.MethodGet, p, nil, &s); err != nil {
		var se *StatusError
		if errors.As(err, &se) && se.Code == http.StatusNotFound {
			return nil, protocol.KeysPayload{}, ErrNoKeyEndpoint
		}
		return nil, protocol.KeysPayload{}, err
	}
	var payload protocol.KeysPayload
	if err := json.Unmarshal(s.Payload, &payload); err != nil {
		return nil, protocol.KeysPayload{}, fmt.Errorf("upload: key list: %w", err)
	}
	return &s, payload, nil
}

// Register redeems a registration code. It sends no token.
func Register(ctx context.Context, server string, req protocol.RegisterRequest) (protocol.RegisterResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return protocol.RegisterResponse{}, err
	}
	var out protocol.RegisterResponse
	err = doJSON(ctx, NewClient(), Options{ServerURL: server}, http.MethodPost, protocol.RegisterPath, body, &out)
	return out, err
}
