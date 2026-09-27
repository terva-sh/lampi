package cli

import (
	"runtime/debug"
	"testing"
)

func TestBuildInfoVersion(t *testing.T) {
	for _, tc := range []struct {
		name      string
		info      debug.BuildInfo
		ver, comm string
	}{
		{"tagged release", debug.BuildInfo{Main: debug.Module{Version: "v0.1.0"}, Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef0123"}, {Key: "vcs.modified", Value: "false"}}}, "v0.1.0", "0123456789ab"},
		{"modified tree", debug.BuildInfo{Main: debug.Module{Version: "v0.1.1-0.20260927000000-0123456789ab+dirty"}, Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef"}, {Key: "vcs.modified", Value: "true"}}}, "v0.1.1-0.20260927000000-0123456789ab+dirty", "0123456789ab-modified"},
		{"devel without vcs", debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, "0.0.0", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, c := buildInfoVersion(&tc.info)
			if v != tc.ver || c != tc.comm {
				t.Fatalf("got %q %q, want %q %q", v, c, tc.ver, tc.comm)
			}
		})
	}
}
