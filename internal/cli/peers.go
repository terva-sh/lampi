package cli

import (
	"context"
	"fmt"
	"os"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/adapter/claude"
	"terva.sh/lampi/internal/adapter/codex"
	"terva.sh/lampi/internal/adapter/cursor"
	"terva.sh/lampi/internal/adapter/opencode"
	"terva.sh/lampi/internal/adapter/terva"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/watch"
)

// source is one harness home the agent can see. required is terva:
// a missing directory is still watched, and Run reports it. Claude,
// Codex, OpenCode, and the Cursor IDE are skipped when the directory
// is not there.
type source struct {
	harness  adapter.Harness
	home     string
	required bool
}

// sources resolves terva, Claude Code, Codex, OpenCode, and the Cursor
// IDE. terva's home is required. An optional harness whose default
// cannot be named, because HOME is unset and the override is unset, is
// left out. A set override is kept even when the directory does not
// exist yet; Discover treats that as an empty tree.
func sources(getenv func(string) string) ([]source, error) {
	list := []source{
		{harness: terva.Adapter{}, required: true},
		{harness: claude.Adapter{}, required: false},
		{harness: codex.Adapter{}, required: false},
		{harness: opencode.Adapter{}, required: false},
		{harness: cursor.Adapter{}, required: false},
	}
	var out []source
	for _, s := range list {
		home, err := s.harness.Home(getenv)
		if err != nil {
			if s.required {
				return nil, err
			}
			continue
		}
		s.home = home
		out = append(out, s)
	}
	return out, nil
}

func (s source) layout() watch.Layout {
	return watch.Layout{Dir: s.harness.WatchDir(), Match: s.harness.Match}
}

// watchDirs is every directory under the harness home that holds
// artifacts. A harness with one tree returns WatchDir.
func (s source) watchDirs() []string {
	if roots, ok := s.harness.(adapter.WatchRoots); ok {
		if dirs := roots.WatchDirs(); len(dirs) > 0 {
			return dirs
		}
	}
	return []string{s.harness.WatchDir()}
}

// watch reports whether Run should be started. A missing optional home
// is not an error: that harness is not installed. A missing terva home
// is still started so the watcher reports it.
func (s source) watch() bool {
	st, err := os.Stat(s.home)
	if err != nil || !st.IsDir() {
		return s.required
	}
	return true
}

func homeLabel(name string) string {
	switch name {
	case protocol.HarnessTerva:
		return "terva_home"
	case protocol.HarnessClaude:
		return "claude_config_dir"
	case protocol.HarnessCodex:
		return "codex_home"
	case protocol.HarnessOpenCode:
		return "opencode_data_dir"
	case protocol.HarnessCursor:
		return "cursor_user_data"
	default:
		return name + "_home"
	}
}

type foundFile struct {
	harness string
	ref     adapter.Ref
}

func discoverSources(ctx context.Context, src []source) ([]foundFile, error) {
	var out []foundFile
	for _, s := range src {
		refs, err := s.harness.Discover(ctx, s.home)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", s.harness.Name(), err)
		}
		for _, r := range refs {
			out = append(out, foundFile{harness: s.harness.Name(), ref: r})
		}
	}
	return out, nil
}

func homeOf(src []source, name string) string {
	for _, s := range src {
		if s.harness.Name() == name {
			return s.home
		}
	}
	return ""
}
