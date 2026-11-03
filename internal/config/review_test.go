package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReviewPrefixSpecificityAndCaptureIndependence(t *testing.T) {
	review := ReviewConfig{Rules: []ReviewRule{{"/tmp", "hide"}, {"/tmp/keep", "show"}, {"/tmp/keep/private", "hide"}}}
	for _, tc := range []struct {
		name, cwd string
		allow     []ProjectMatch
		hidden    bool
	}{
		{"root", "/tmp", nil, true},
		{"future", "/tmp/new-session", nil, true},
		{"boundary", "/tmp-other/new", nil, false},
		{"normalized path", "/srv/../tmp/new", nil, true},
		{"show exception", "/tmp/keep/app", nil, false},
		{"deeper hide", "/tmp/keep/private/app", nil, true},
		{"narrow allow", "/tmp/wanted/app", []ProjectMatch{{CWDPrefix: "/tmp/wanted"}}, false},
		{"equal allow", "/tmp/new", []ProjectMatch{{CWDPrefix: "/tmp"}}, true},
		{"broad allow", "/tmp/new", []ProjectMatch{{CWDPrefix: "/"}}, true},
		{"unrelated allow", "/tmp/new", []ProjectMatch{{CWDPrefix: "/srv"}}, true},
		{"unknown cwd", "", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := ProjectID{CWD: tc.cwd}
			_, got := review.HiddenBy(id, tc.allow)
			if got != tc.hidden {
				t.Fatalf("hidden=%v, want %v", got, tc.hidden)
			}
			p := Profile{Projects: Projects{Allow: tc.allow, Deny: []ProjectMatch{{CWDPrefix: "/tmp"}}}, Review: review}
			if p.Projects.Permitted(id) != p.DeviceProfile().Projects.Permitted(id) {
				t.Fatal("visibility changed capture")
			}
		})
	}
	for _, rules := range [][]ReviewRule{{{"/tmp", "show"}, {"/tmp", "hide"}}, {{"/tmp", "hide"}, {"/tmp", "show"}}} {
		if _, hidden := (ReviewConfig{Rules: rules}).HiddenBy(ProjectID{CWD: "/tmp/x"}, nil); !hidden {
			t.Fatal("hide must win a tie")
		}
	}
	// Additional constraints at the same prefix are also narrower.
	if _, hidden := review.HiddenBy(ProjectID{CWD: "/tmp/x", GitRemote: "git.example/team/keep"}, []ProjectMatch{{CWDPrefix: "/tmp", GitRemote: "git.example/team/keep"}}); hidden {
		t.Fatal("compound exception hidden")
	}
}

func TestReviewValidationAndDeviceDocument(t *testing.T) {
	for _, raw := range []string{
		`{"review":{"rules":[{"cwd_prefix":"tmp","action":"hide"}]}}`,
		`{"review":{"rules":[{"cwd_prefix":"/tmp/../srv","action":"hide"}]}}`,
		`{"review":{"rules":[{"cwd_prefix":"/tmp","action":"allow"}]}}`,
		`{"review":{"rules":[{"cwd_prefix":"/tmp","action":"hide","unknown":1}]}}`,
	} {
		if _, err := ParseProfile([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	p, err := ParseProfile([]byte(`{"projects":{"allow":[{"cwd_prefix":"/srv"}]},"review":{"rules":[{"cwd_prefix":"/tmp","action":"hide"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(p.DeviceProfile())
	if strings.Contains(string(raw), "review") {
		t.Fatalf("lake-only field in device document: %s", raw)
	}
	if p.Version() == p.DeviceProfile().Version() {
		t.Fatal("stored revision did not retain review policy")
	}
	if got := p.DeviceProfile().Version(); got != (Profile{Projects: p.Projects}).Version() {
		t.Fatal("review policy changed device version")
	}
}
