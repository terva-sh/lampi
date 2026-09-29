package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestBayRequestsFor(t *testing.T) {
	b := BayRequests{
		Rules: []BayRequestRule{
			{ProjectMatch: ProjectMatch{CWDPrefix: "/src/client"}, Bays: []string{"client-x", "billing"}},
			{Harness: "claude", Bays: []string{"agents", "client-x"}},
			{ProjectMatch: ProjectMatch{GitRemotePrefix: "git@host:org"}, Harness: "codex", Bays: []string{"org"}},
		},
		Default: []string{"personal"},
	}
	for _, tc := range []struct {
		name, harness string
		id            ProjectID
		want          BayChoice
	}{
		{"one rule, two bays", "terva", ProjectID{CWD: "/src/client/app"}, BayChoice{Bays: []string{"client-x", "billing"}, Rules: []int{0}}},
		{"two rules, deduped", "claude", ProjectID{CWD: "/src/client"}, BayChoice{Bays: []string{"client-x", "billing", "agents"}, Rules: []int{0, 1}}},
		{"harness alone", "claude", ProjectID{CWD: "/elsewhere"}, BayChoice{Bays: []string{"agents", "client-x"}, Rules: []int{1}}},
		{"sibling folder", "terva", ProjectID{CWD: "/src/client-y"}, BayChoice{Bays: []string{"personal"}, Default: true}},
		{"harness must match too", "terva", ProjectID{CWD: "/r", GitRemote: "git@host:org/repo.git"}, BayChoice{Bays: []string{"personal"}, Default: true}},
		{"remote and harness", "codex", ProjectID{CWD: "/r", GitRemote: "git@host:org/repo.git"}, BayChoice{Bays: []string{"org"}, Rules: []int{2}}},
	} {
		if got := b.For(tc.harness, tc.id); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %+v want %+v", tc.name, got, tc.want)
		}
	}
	if got := (BayRequests{}).For("terva", ProjectID{CWD: "/x"}); len(got.Bays) != 0 || got.Default {
		t.Errorf("no config asks for %+v", got)
	}
}

func TestBayRequestsValidate(t *testing.T) {
	for name, tc := range map[string]struct {
		b    BayRequests
		want string
	}{
		"matches nothing": {BayRequests{Rules: []BayRequestRule{{Bays: []string{"a"}}}}, "set a project field or a harness"},
		"no bays":         {BayRequests{Rules: []BayRequestRule{{Harness: "claude"}}}, "bays is empty"},
		"bad name":        {BayRequests{Rules: []BayRequestRule{{Harness: "claude", Bays: []string{"Client X"}}}}, "not a bay name"},
		"bad default":     {BayRequests{Default: []string{""}}, "bays.default"},
		"bad glob":        {BayRequests{Rules: []BayRequestRule{{ProjectMatch: ProjectMatch{CWDGlob: "rel/*"}, Bays: []string{"a"}}}}, "cwd_glob"},
	} {
		if err := tc.b.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	ok := BayRequests{Rules: []BayRequestRule{{ProjectMatch: ProjectMatch{CWDPrefix: "/x"}, Bays: []string{"a", "bay_0123abcd"}}}, Default: []string{"b"}}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
}
