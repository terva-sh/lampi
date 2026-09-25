package cli

import (
	"sync"
	"time"
)

// debouncer holds watch events until the watch has been quiet for
// window, then calls fire once. A change is never held longer than
// longest after the first event of the burst, so a file that is
// written without a pause still syncs. A window of zero calls fire on
// every event.
type debouncer struct {
	window, longest time.Duration
	fire            func()

	mu    sync.Mutex
	timer *time.Timer
	first time.Time
	// gen tells a timer that was replaced from the current one. Stop
	// cannot recall a callback that has already started.
	gen int
}

func newDebouncer(window, longest time.Duration, fire func()) *debouncer {
	return &debouncer{window: window, longest: max(window, longest), fire: fire}
}

// touch is one watch event.
func (d *debouncer) touch() {
	if d.window <= 0 {
		d.fire()
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	if d.timer == nil {
		d.first = now
	} else {
		d.timer.Stop()
	}
	wait := min(d.window, d.first.Add(d.longest).Sub(now))
	d.gen++
	gen := d.gen
	d.timer = time.AfterFunc(max(wait, 0), func() {
		d.mu.Lock()
		if gen != d.gen {
			d.mu.Unlock()
			return
		}
		d.timer = nil
		d.mu.Unlock()
		d.fire()
	})
}

// stop drops a burst that has not fired.
func (d *debouncer) stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	d.gen++
}
