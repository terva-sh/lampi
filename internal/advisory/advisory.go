// Package advisory holds what this build of the lake knows about agent
// releases: which ones to upgrade from, how urgently, and why. It ships
// inside the lake, so upgrading the lake is enough to warn about agents
// already in the field.
package advisory

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"terva.sh/lampi/internal/release"
)

// Severity is how strongly an advisory asks for an upgrade.
type Severity string

const (
	// Upgrade asks for an upgrade when convenient.
	Upgrade Severity = "upgrade"
	// Urgent is a release known to lose, leak or corrupt data, or to be
	// incompatible with this lake. The dashboard raises it on every page
	// that lists devices.
	Urgent Severity = "urgent"
)

// Advisory covers the agent releases from Introduced up to, and not
// including, Fixed. An empty Fixed covers every later release.
type Advisory struct {
	Introduced string   `json:"introduced"`
	Fixed      string   `json:"fixed,omitempty"`
	Severity   Severity `json:"severity"`
	// Reason is one sentence an operator reads beside the device.
	Reason string `json:"reason"`
	// Link is the release note or ticket that explains it.
	Link string `json:"link,omitempty"`

	from, to release.Version
}

// Set is a parsed advisory file.
type Set struct{ list []Advisory }

//go:embed agents.json
var agentsJSON []byte

// Agents is the advisories this build of the lake ships with.
var Agents = mustParse(agentsJSON)

func mustParse(raw []byte) Set {
	s, err := Parse(raw)
	if err != nil {
		panic(err)
	}
	return s
}

// Parse reads an advisory file, {"agents": [ADVISORY, ...]}, and
// refuses one it cannot apply exactly.
func Parse(raw []byte) (Set, error) {
	var f struct {
		Agents *[]Advisory `json:"agents"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return Set{}, fmt.Errorf("advisory: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Set{}, errors.New("advisory: data after the object")
	}
	if f.Agents == nil {
		return Set{}, errors.New("advisory: no agents list")
	}
	list := *f.Agents
	for i := range list {
		a := &list[i]
		var ok bool
		if a.from, ok = release.Parse(a.Introduced); !ok {
			return Set{}, fmt.Errorf("advisory %d: introduced %q is not a release", i, a.Introduced)
		}
		if a.Fixed != "" {
			if a.to, ok = release.Parse(a.Fixed); !ok {
				return Set{}, fmt.Errorf("advisory %d: fixed %q is not a release", i, a.Fixed)
			}
			if a.to.Compare(a.from) <= 0 {
				return Set{}, fmt.Errorf("advisory %d: fixed %s is not after introduced %s", i, a.Fixed, a.Introduced)
			}
		}
		if a.Severity != Upgrade && a.Severity != Urgent {
			return Set{}, fmt.Errorf("advisory %d: severity %q is not upgrade or urgent", i, a.Severity)
		}
		if strings.TrimSpace(a.Reason) == "" {
			return Set{}, fmt.Errorf("advisory %d: no reason", i)
		}
		if a.Link != "" && !strings.HasPrefix(a.Link, "https://") {
			return Set{}, fmt.Errorf("advisory %d: link %q is not https", i, a.Link)
		}
	}
	return Set{list: list}, nil
}

// Match is the advisory that covers v, the first urgent one if any
// covers it, else the first. An unstamped build matches nothing.
func (s Set) Match(version string) (Advisory, bool) {
	v, ok := release.Parse(version)
	if !ok {
		return Advisory{}, false
	}
	var found Advisory
	var any bool
	for _, a := range s.list {
		if v.Compare(a.from) < 0 || a.Fixed != "" && v.Compare(a.to) >= 0 {
			continue
		}
		if a.Severity == Urgent {
			return a, true
		}
		if !any {
			found, any = a, true
		}
	}
	return found, any
}
