// Package reqlog carries a handler's error to the access log line the
// lake writes for its request. The lake's HTTP layer adds a slot to
// each request; a handler in any package that writes its own error body
// records the error there, so a 500 in the log says why.
package reqlog

import (
	"context"
	"net/http"
)

type key struct{}

// Slot holds the error a handler recorded for one request.
type Slot struct {
	Err error
}

// With returns r carrying a new slot, and the slot.
func With(r *http.Request) (*http.Request, *Slot) {
	s := &Slot{}
	return r.WithContext(context.WithValue(r.Context(), key{}, s)), s
}

// Note records err on r's slot. A request without one, as in a test
// that calls a handler directly, keeps nothing.
func Note(r *http.Request, err error) {
	if s, ok := r.Context().Value(key{}).(*Slot); ok {
		s.Err = err
	}
}
