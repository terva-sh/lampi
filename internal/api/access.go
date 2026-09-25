package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"terva.sh/lampi/internal/protocol"
)

// Body caps. A JSON body over its cap is 413. blobs/check carries one
// digest per blob, so a first sync sends many; it has its own cap and a
// digest count the client can batch to.
const (
	maxJSONBytes    int64 = 1 << 20
	maxCheckBytes   int64 = 8 << 20
	maxCheckDigests       = 100000
)

// deadlines is the per-request time budget. The http.Server has no
// ReadTimeout or WriteTimeout, so a slow blob PUT is not cut off after
// its body is stored. Each request gets a read deadline of floor plus
// its body size at rate, and a write deadline slack after that.
type deadlines struct {
	json  time.Duration // floor for every route but a blob PUT
	blob  time.Duration // floor for PUT /v1/blobs/{digest}
	rate  int64         // slowest body the budget allows, bytes per second
	slack time.Duration // time to answer after the read deadline
}

var defaultDeadlines = deadlines{
	json:  time.Minute,
	blob:  2 * time.Minute,
	rate:  64 << 10,
	slack: time.Minute,
}

func (s *Server) deadlines() deadlines {
	if s.limits != nil {
		return *s.limits
	}
	return defaultDeadlines
}

// budget is the read time for r. A blob PUT is sized from Content-Length,
// or from max_blob_bytes when the length is unknown or larger.
func (d deadlines) budget(r *http.Request) time.Duration {
	floor, size := d.json, maxJSONBytes
	switch {
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/v1/blobs/"):
		floor, size = d.blob, protocol.MaxBlobBytes
	case r.Method == http.MethodPost && r.URL.Path == "/v1/blobs/check":
		size = maxCheckBytes
	}
	if r.ContentLength >= 0 && r.ContentLength < size {
		size = r.ContentLength
	}
	if d.rate <= 0 {
		return floor
	}
	return floor + time.Duration(size)*time.Second/time.Duration(d.rate)
}

// requestInfo is what the access log reads after the handler returns.
type requestInfo struct {
	body *countingBody
	err  error
}

type requestInfoKey struct{}

func infoOf(r *http.Request) *requestInfo {
	info, _ := r.Context().Value(requestInfoKey{}).(*requestInfo)
	return info
}

// serveHTTP wraps the mux. It counts the request for Close, sets its
// deadlines, and writes one access log line. Every request sets both
// deadlines: with WriteTimeout 0 net/http does not reset the write
// deadline between requests on a kept-alive connection.
func (s *Server) serveHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.active.Add(1)
		defer s.active.Done()
		start := time.Now()

		d := s.deadlines()
		read := start.Add(d.budget(r))
		rc := http.NewResponseController(w)
		// A recorder in tests does not support deadlines. The server does.
		_ = rc.SetReadDeadline(read)
		_ = rc.SetWriteDeadline(read.Add(d.slack))

		aw := &accessWriter{ResponseWriter: w}
		info := &requestInfo{body: &countingBody{ReadCloser: r.Body}}
		r = r.WithContext(context.WithValue(r.Context(), requestInfoKey{}, info))
		r.Body = info.body
		next.ServeHTTP(aw, r)
		s.logRequest(r, aw, info, time.Since(start))
	})
}

// logRequest writes one line per request. A 200 /healthz is skipped so
// a process probe does not fill the journal. Authorization is not read.
// X-Forwarded-For is logged as the proxy sent it, not trusted.
func (s *Server) logRequest(r *http.Request, aw *accessWriter, info *requestInfo, took time.Duration) {
	status := aw.status
	if status == 0 {
		status = http.StatusOK
	}
	if r.URL.Path == "/healthz" && status == http.StatusOK {
		return
	}
	attrs := []slog.Attr{
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.Int("status", status),
		slog.Int64("bytes", aw.bytes),
		slog.Int64("body_bytes", info.body.n),
		slog.Duration("duration", took),
		slog.String("remote", r.RemoteAddr),
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		attrs = append(attrs, slog.String("xff", xff))
	}
	level := slog.LevelInfo
	if status >= 500 {
		level = slog.LevelError
	}
	if info.err != nil {
		attrs = append(attrs, slog.String("err", info.err.Error()))
	}
	s.logger().LogAttrs(context.Background(), level, "request", attrs...)
}

func (s *Server) logger() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.New(slog.DiscardHandler)
}

// fail writes an error body and keeps err for the access log. A 5xx
// body is a fixed message: the detail can name a lake path, so it stays
// in the server log. A failure after the request body could not be read
// is the request's, not the lake's. The log keeps that mapped error,
// not the lake's wrapping of it.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, code int, err error) {
	info := infoOf(r)
	if code >= 500 && info != nil && info.body.err != nil {
		code, err = bodyStatus(info.body.err)
	}
	if info != nil {
		info.err = err
	}
	if code >= 500 {
		writeJSON(w, code, protocol.ErrorBody{Error: "internal error; see the server log"})
		return
	}
	writeJSON(w, code, protocol.ErrorBody{Error: err.Error()})
}

// note keeps err for the access log when the handler writes its own body.
func note(r *http.Request, err error) {
	if info := infoOf(r); info != nil {
		info.err = err
	}
}

// bodyStatus maps a failed read of the request body. The read error
// names addresses, not lake paths, but the body stays a fixed message.
func bodyStatus(err error) (int, error) {
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		return http.StatusRequestEntityTooLarge, errors.New("request body is too large")
	case errors.Is(err, os.ErrDeadlineExceeded):
		return http.StatusRequestTimeout, errors.New("request body timed out")
	}
	return http.StatusBadRequest, errors.New("request body could not be read")
}

// accessWriter records the status and response bytes.
type accessWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (a *accessWriter) WriteHeader(code int) {
	if a.status == 0 {
		a.status = code
	}
	a.ResponseWriter.WriteHeader(code)
}

func (a *accessWriter) Write(p []byte) (int, error) {
	if a.status == 0 {
		a.status = http.StatusOK
	}
	n, err := a.ResponseWriter.Write(p)
	a.bytes += int64(n)
	return n, err
}

// Unwrap lets http.NewResponseController reach the connection.
func (a *accessWriter) Unwrap() http.ResponseWriter { return a.ResponseWriter }

// countingBody records request bytes read and the first read error
// other than EOF.
type countingBody struct {
	io.ReadCloser
	n   int64
	err error
}

func (b *countingBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.n += int64(n)
	if err != nil && err != io.EOF && b.err == nil {
		b.err = err
	}
	return n, err
}
