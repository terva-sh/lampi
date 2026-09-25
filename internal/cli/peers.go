package cli

import (
	"context"
	"fmt"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/adapter/claude"
	"terva.sh/lampi/internal/adapter/codex"
	"terva.sh/lampi/internal/adapter/cursor"
	"terva.sh/lampi/internal/adapter/cursorcli"
	"terva.sh/lampi/internal/adapter/opencode"
	"terva.sh/lampi/internal/adapter/terva"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/watch"
)

// source is one harness home the agent will read. required is terva
// when that harness is enabled: a home that cannot be named, because
// HOME is unset, is an error for terva and a skip for the others. A
// home that can be named but does not exist yet is watched either way;
// the watcher polls until it appears. enabled false omits the harness
// before that check, including terva, so a disabled terva home is not
// watched and is not an error.
type source struct {
	harness  adapter.Harness
	home     string
	required bool
}

// knownSources is the harness set the agent already wires. Order is
// the order discover, watch, and upload report them.
func knownSources() []source {
	return []source{
		{harness: terva.Adapter{}, required: true},
		{harness: claude.Adapter{}, required: false},
		{harness: codex.Adapter{}, required: false},
		{harness: opencode.Adapter{}, required: false},
		{harness: cursor.Adapter{}, required: false},
		{harness: cursorcli.Adapter{}, required: false},
	}
}

// configuredSources loads config.json and resolves the harness homes
// the agent will discover, watch, and upload.
func configuredSources(getenv func(string) string) ([]source, error) {
	file, err := config.LoadFile(getenv)
	if err != nil {
		return nil, err
	}
	return sources(getenv, file.Harnesses)
}

// sources resolves terva, Claude Code, Codex, OpenCode, the Cursor IDE,
// and the Cursor CLI. harnesses is the Shape A map. A nil map leaves
// every harness on. enabled false drops that harness only. terva's
// home is required when terva is on. An optional harness whose default
// cannot be named, because HOME is unset and the override is unset, is
// left out. A set override is kept even when the directory does not
// exist yet; Discover treats that as an empty tree.
func sources(getenv func(string) string, harnesses config.Harnesses) ([]source, error) {
	var out []source
	for _, s := range knownSources() {
		e, ok := harnesses[s.harness.Name()]
		var entry *config.HarnessConfig
		if ok {
			entry = &e
		}
		// No per-harness root flag exists. The empty string is that
		// absent flag, so config root, then env, then the adapter
		// default still apply.
		home, skip, err := resolveHome(s.harness.Name(), entry, "", getenv, s.harness.Home)
		if skip {
			continue
		}
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

// resolveHome applies the locked root order: flagRoot, then the config
// root, then adapterHome. adapterHome is the harness Home function. It
// still applies the harness env var, then the adapter default, and it
// does not read config. flagRoot is empty until a CLI flag exists.
// cfgEntry nil means the harness key was omitted: enabled, no root.
// skip is true when the entry exists and Enabled is false. The home is
// still resolved in that case so a later status report can show it.
// sources drops a skip and does not surface the error.
func resolveHome(id string, cfgEntry *config.HarnessConfig, flagRoot string, getenv func(string) string, adapterHome func(func(string) string) (string, error)) (home string, skip bool, err error) {
	if id == "" {
		return "", false, fmt.Errorf("harness id is empty")
	}
	skip = cfgEntry != nil && !cfgEntry.Enabled
	if flagRoot != "" {
		return flagRoot, skip, nil
	}
	if cfgEntry != nil && cfgEntry.Root != "" {
		return cfgEntry.Root, skip, nil
	}
	home, err = adapterHome(getenv)
	if err != nil {
		return "", skip, err
	}
	return home, skip, nil
}

// harnessStatus is one known harness as operator status prints it.
// enabled is false only when the config entry exists and sets it false.
// An omitted map or key stays enabled. root is the directory from
// resolveHome, or empty when that directory cannot be named. source is
// config, env, or default.
type harnessStatus struct {
	id      string
	enabled bool
	root    string
	source  string
}

// line is the stable status row. Ops docs can cite this spelling.
func (h harnessStatus) line() string {
	return fmt.Sprintf("harness %s enabled=%t root=%s source=%s", h.id, h.enabled, h.root, h.source)
}

// harnessStatuses reports every known harness in knownSources order.
// A disabled harness stays in the slice. sources drops that harness.
// flagRoot is empty: no per-harness root flag exists.
func harnessStatuses(getenv func(string) string, harnesses config.Harnesses) []harnessStatus {
	known := knownSources()
	out := make([]harnessStatus, 0, len(known))
	for _, s := range known {
		out = append(out, oneHarnessStatus(s, getenv, harnesses))
	}
	return out
}

func oneHarnessStatus(s source, getenv func(string) string, harnesses config.Harnesses) harnessStatus {
	id := s.harness.Name()
	var entry *config.HarnessConfig
	if e, ok := harnesses[id]; ok {
		entry = &e
	}
	home, skip, err := resolveHome(id, entry, "", getenv, s.harness.Home)
	root := ""
	if err == nil {
		root = home
	}
	return harnessStatus{
		id:      id,
		enabled: !skip,
		root:    root,
		source:  harnessResolution(entry, id, getenv),
	}
}

// harnessResolution names which layer won. config root wins over the
// harness override, and the override wins over the adapter default.
// The flag slot is empty, so it is not a layer here.
func harnessResolution(entry *config.HarnessConfig, id string, getenv func(string) string) string {
	if entry != nil && entry.Root != "" {
		return "config"
	}
	if harnessOverride(id, getenv) {
		return "env"
	}
	return "default"
}

// harnessOverride reports the harness environment variable that beats
// the adapter default. Cursor has none: XDG_CONFIG_HOME and APPDATA
// select the platform directory, which is the default. XDG_CONFIG_HOME
// is that same kind of input for the Cursor CLI. XDG_STATE_HOME is
// that input for terva. Those are not overrides.
func harnessOverride(id string, getenv func(string) string) bool {
	switch id {
	case protocol.HarnessTerva:
		return getenv("TERVA_HOME") != "" || getenv("ZOT_HOME") != ""
	case protocol.HarnessClaude:
		return getenv("CLAUDE_CONFIG_DIR") != ""
	case protocol.HarnessCodex:
		return getenv("CODEX_HOME") != ""
	case protocol.HarnessOpenCode:
		return getenv("XDG_DATA_HOME") != ""
	case protocol.HarnessCursorCLI:
		return getenv("CURSOR_CONFIG_DIR") != ""
	default:
		return false
	}
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
	case protocol.HarnessCursorCLI:
		return "cursor_cli_config"
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
