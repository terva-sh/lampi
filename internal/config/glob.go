package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// A cwd_glob names a directory layout rather than one path, so one rule
// covers it on every machine: /home/*/notes, or /home/*/.t3/worktrees/**.
// It is an absolute path split on "/". A "*" inside a segment matches
// any run of characters but "/", and a segment that is exactly "**"
// matches any number of whole segments, none included. Every other
// character is literal, so a path holding "[" or "?" means what it says,
// as in cwd_prefix. Like cwd_prefix it matches the directory it names and
// everything under it. TKT-01M3NM01S.

// maxGlobStars bounds the "**" segments of one pattern, which keeps a
// match cheap and a pattern readable.
const maxGlobStars = 4

// checkGlob says why pattern cannot be a cwd_glob, or nil. A pattern is
// absolute, has no empty, "." or ".." segment, and holds at least one
// segment with no "*", so no pattern matches every directory.
func checkGlob(pattern string) error {
	if !strings.HasPrefix(pattern, "/") {
		return errors.New("a cwd_glob is an absolute path starting with /")
	}
	segs := globSegments(pattern)
	literal, stars := false, 0
	for _, s := range segs {
		switch {
		case s == "" || s == "." || s == "..":
			return errors.New("a cwd_glob has no empty, . or .. segment")
		case s == "**":
			stars++
		case strings.Contains(s, "**"):
			return errors.New("** in a cwd_glob is a whole segment")
		case !strings.Contains(s, "*"):
			literal = true
		}
	}
	if !literal {
		return errors.New("a cwd_glob needs a segment with no *, so that it cannot match every directory")
	}
	if stars > maxGlobStars {
		return fmt.Errorf("a cwd_glob has at most %d ** segments", maxGlobStars)
	}
	return nil
}

// globSegments is pattern's segments, less its leading "/" and one
// trailing "/".
func globSegments(pattern string) []string {
	return strings.Split(strings.TrimSuffix(strings.TrimPrefix(pattern, "/"), "/"), "/")
}

// globHasPrefix reports whether pattern matches cwd or a directory cwd
// is under. fold ignores case, as a deny rule does. An empty cwd, and a
// pattern checkGlob refuses, match nothing; a deny rule reads the second
// as a match itself.
func globHasPrefix(cwd, pattern string, fold bool) bool {
	if cwd == "" || checkGlob(pattern) != nil {
		return false
	}
	cwd = filepath.ToSlash(filepath.Clean(cwd))
	if !strings.HasPrefix(cwd, "/") {
		return false
	}
	if fold {
		cwd, pattern = strings.ToLower(cwd), strings.ToLower(pattern)
	}
	ps := globSegments(pattern)
	cs := strings.Split(strings.TrimPrefix(cwd, "/"), "/")
	if cwd == "/" {
		cs = nil
	}
	// m[i][j]: ps[i:] matches some leading run of cs[j:].
	m := make([][]bool, len(ps)+1)
	for i := range m {
		m[i] = make([]bool, len(cs)+1)
	}
	for j := range cs {
		m[len(ps)][j] = true
	}
	m[len(ps)][len(cs)] = true
	for i := len(ps) - 1; i >= 0; i-- {
		for j := len(cs); j >= 0; j-- {
			switch {
			case ps[i] == "**":
				m[i][j] = m[i+1][j] || j < len(cs) && m[i][j+1]
			case j < len(cs):
				m[i][j] = segmentMatch(ps[i], cs[j]) && m[i+1][j+1]
			}
		}
	}
	return m[0][0]
}

// segmentMatch reports whether one pattern segment matches one path
// segment: "*" is any run of characters, every other byte is itself.
func segmentMatch(pattern, seg string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == seg
	}
	if !strings.HasPrefix(seg, parts[0]) {
		return false
	}
	seg = seg[len(parts[0]):]
	last := parts[len(parts)-1]
	for _, p := range parts[1 : len(parts)-1] {
		i := strings.Index(seg, p)
		if i < 0 {
			return false
		}
		seg = seg[i+len(p):]
	}
	return len(seg) >= len(last) && strings.HasSuffix(seg, last)
}
