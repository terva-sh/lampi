package config

import (
	"fmt"
	"time"
)

const (
	// DefaultDebounce is how long the watch must be quiet before the
	// agent syncs.
	DefaultDebounce = 5 * time.Second
	// DefaultDebounceMax is the longest a change waits for that quiet,
	// so a file written without a pause still syncs.
	DefaultDebounceMax = 30 * time.Second
)

// AgentConfig tunes the long-running agent. Each value is a Go
// duration string such as "5s". An empty value is the default.
type AgentConfig struct {
	Debounce    string `json:"debounce,omitempty"`
	DebounceMax string `json:"debounce_max,omitempty"`
}

// Windows is the quiet window and the longest wait. A window of zero
// syncs on every change. The longest wait is at least the window.
func (a AgentConfig) Windows() (window, longest time.Duration, err error) {
	window, err = duration("agent.debounce", a.Debounce, DefaultDebounce)
	if err != nil {
		return 0, 0, err
	}
	longest, err = duration("agent.debounce_max", a.DebounceMax, DefaultDebounceMax)
	if err != nil {
		return 0, 0, err
	}
	return window, max(window, longest), nil
}

func duration(name, s string, def time.Duration) (time.Duration, error) {
	if s == "" {
		return def, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("config: %s: %w", name, err)
	}
	if d < 0 {
		return 0, fmt.Errorf("config: %s: %s is negative", name, s)
	}
	return d, nil
}
