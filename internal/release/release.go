// Package release reads and compares terva-lampi release versions,
// for self-update and for the advice status gives when an agent is
// behind its lake.
package release

import (
	"strconv"
	"strings"
)

// Version is a release version, vMAJOR.MINOR.PATCH.
type Version struct{ Major, Minor, Patch int }

// Parse reads v0.8.0 or 0.8.0. A prerelease or build suffix, a dev
// build's 0.0.0, and anything else that is not a plain release fail:
// none of them is a release that can be compared.
func Parse(s string) (Version, bool) {
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) != 3 {
		return Version{}, false
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 || p != strconv.Itoa(v) {
			return Version{}, false
		}
		n[i] = v
	}
	v := Version{n[0], n[1], n[2]}
	if v == (Version{}) {
		return Version{}, false
	}
	return v, true
}

func (v Version) String() string {
	return "v" + strconv.Itoa(v.Major) + "." + strconv.Itoa(v.Minor) + "." + strconv.Itoa(v.Patch)
}

// Compare is -1, 0 or 1 as v is older than, the same as, or newer than w.
func (v Version) Compare(w Version) int {
	for _, d := range [3]int{v.Major - w.Major, v.Minor - w.Minor, v.Patch - w.Patch} {
		switch {
		case d < 0:
			return -1
		case d > 0:
			return 1
		}
	}
	return 0
}

// Gap names the highest part by which to is newer than from: none,
// patch, minor or major. A to at or behind from is none.
func Gap(from, to Version) string {
	switch {
	case to.Compare(from) <= 0:
		return "none"
	case to.Major != from.Major:
		return "major"
	case to.Minor != from.Minor:
		return "minor"
	default:
		return "patch"
	}
}
