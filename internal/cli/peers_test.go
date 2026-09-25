package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/adapter/cursor"
	"terva.sh/lampi/internal/adapter/cursorcli"
	"terva.sh/lampi/internal/adapter/opencode"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/watch"
)

// harnessFixture is a getenv where every wired harness can name a home.
// Cursor has no override variable; its default follows HOME.
type harnessFixture struct {
	getenv    func(string) string
	home      string
	terva     string
	claude    string
	codex     string
	xdgData   string
	cursorCLI string
}

func newHarnessFixture(t *testing.T) harnessFixture {
	t.Helper()
	f := harnessFixture{
		home:      t.TempDir(),
		terva:     t.TempDir(),
		claude:    t.TempDir(),
		codex:     t.TempDir(),
		xdgData:   t.TempDir(),
		cursorCLI: t.TempDir(),
	}
	f.getenv = func(k string) string {
		switch k {
		case "HOME":
			return f.home
		case "TERVA_HOME":
			return f.terva
		case "CLAUDE_CONFIG_DIR":
			return f.claude
		case "CODEX_HOME":
			return f.codex
		case "XDG_DATA_HOME":
			return f.xdgData
		case "CURSOR_CONFIG_DIR":
			return f.cursorCLI
		default:
			return ""
		}
	}
	return f
}

func sourceNames(src []source) map[string]string {
	out := map[string]string{}
	for _, s := range src {
		out[s.harness.Name()] = s.home
	}
	return out
}

func TestSourcesSkipsDisabledClaude(t *testing.T) {
	f := newHarnessFixture(t)
	src, err := sources(f.getenv, config.Harnesses{
		protocol.HarnessClaude: {Enabled: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := sourceNames(src)
	if _, ok := got[protocol.HarnessClaude]; ok {
		t.Fatalf("disabled claude was included: %v", got)
	}
	for _, id := range []string{
		protocol.HarnessTerva,
		protocol.HarnessCodex,
		protocol.HarnessOpenCode,
		protocol.HarnessCursor,
		protocol.HarnessCursorCLI,
	} {
		if got[id] == "" {
			t.Fatalf("missing %s: %v", id, got)
		}
	}
	if got[protocol.HarnessTerva] != f.terva || got[protocol.HarnessCodex] != f.codex || got[protocol.HarnessCursorCLI] != f.cursorCLI {
		t.Fatalf("homes changed for harnesses that were left on: %v", got)
	}
	if got[protocol.HarnessOpenCode] != filepath.Join(f.xdgData, "opencode") {
		t.Fatalf("opencode home %q", got[protocol.HarnessOpenCode])
	}
}

func TestSourcesOmitAndExplicitEnable(t *testing.T) {
	f := newHarnessFixture(t)
	omitted, err := sources(f.getenv, nil)
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := sources(f.getenv, config.Harnesses{
		protocol.HarnessClaude: {Enabled: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		protocol.HarnessTerva,
		protocol.HarnessClaude,
		protocol.HarnessCodex,
		protocol.HarnessOpenCode,
		protocol.HarnessCursor,
		protocol.HarnessCursorCLI,
	}
	for _, src := range [][]source{omitted, explicit} {
		got := sourceNames(src)
		if len(got) != len(want) {
			t.Fatalf("sources %v", got)
		}
		for _, id := range want {
			if got[id] == "" {
				t.Fatalf("missing %s in %v", id, got)
			}
		}
		if got[protocol.HarnessClaude] != f.claude {
			t.Fatalf("claude home %q", got[protocol.HarnessClaude])
		}
	}
}

func TestDiscoverSkipsDisabledHarness(t *testing.T) {
	f := newHarnessFixture(t)
	writeRel(t, f.terva, "sessions/abcd/s.jsonl", "{}\n")
	writeRel(t, f.claude, "projects/p/s.jsonl", "{}\n")

	off, err := sources(f.getenv, config.Harnesses{
		protocol.HarnessClaude: {Enabled: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	files, err := discoverSources(context.Background(), off)
	if err != nil {
		t.Fatal(err)
	}
	sawTerva := false
	for _, file := range files {
		if file.harness == protocol.HarnessClaude {
			t.Fatalf("discover listed disabled claude: %+v", file)
		}
		if file.harness == protocol.HarnessTerva && file.ref.RelPath == "sessions/abcd/s.jsonl" {
			sawTerva = true
		}
	}
	if !sawTerva {
		t.Fatalf("terva file missing: %+v", files)
	}

	on, err := sources(f.getenv, nil)
	if err != nil {
		t.Fatal(err)
	}
	files, err = discoverSources(context.Background(), on)
	if err != nil {
		t.Fatal(err)
	}
	sawClaude := false
	sawTerva = false
	for _, file := range files {
		if file.harness == protocol.HarnessClaude && file.ref.RelPath == "projects/p/s.jsonl" {
			sawClaude = true
		}
		if file.harness == protocol.HarnessTerva {
			sawTerva = true
		}
	}
	if !sawClaude || !sawTerva {
		t.Fatalf("discover %+v", files)
	}
}

func TestStartWatchesSkipsDisabledHarness(t *testing.T) {
	f := newHarnessFixture(t)
	on, err := sources(f.getenv, nil)
	if err != nil {
		t.Fatal(err)
	}
	off, err := sources(f.getenv, config.Harnesses{
		protocol.HarnessClaude: {Enabled: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	all := startWatches(on, false, nil, nil)
	kept := startWatches(off, false, nil, nil)
	if watchersAt(all, f.claude) == 0 {
		t.Fatal("enabled claude produced no watcher")
	}
	if watchersAt(kept, f.claude) != 0 {
		t.Fatalf("disabled claude still watched")
	}
	if watchersAt(kept, f.terva) == 0 || watchersAt(kept, f.terva) != watchersAt(all, f.terva) {
		t.Fatalf("terva watchers on=%d off=%d", watchersAt(all, f.terva), watchersAt(kept, f.terva))
	}
	if watchersAt(kept, f.codex) != watchersAt(all, f.codex) || watchersAt(kept, f.cursorCLI) != watchersAt(all, f.cursorCLI) {
		t.Fatal("another harness lost its watcher")
	}
}

func TestDisabledTervaIsOmitted(t *testing.T) {
	// HOME unset would be an error for an enabled terva. Disabled
	// still has to be a skip, not that error, and not a watcher.
	src, err := sources(func(string) string { return "" }, config.Harnesses{
		protocol.HarnessTerva: {Enabled: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sourceNames(src)[protocol.HarnessTerva]; ok {
		t.Fatal("disabled terva was included")
	}
	if len(startWatches(src, false, nil, nil)) != 0 {
		t.Fatal("disabled terva started a watcher")
	}
}

func TestResolveHomePrecedence(t *testing.T) {
	configRoot := t.TempDir()
	envRoot := t.TempDir()
	homeDir := t.TempDir()
	flagRoot := t.TempDir()
	calls := 0
	adapter := func(getenv func(string) string) (string, error) {
		calls++
		if v := getenv("CLAUDE_CONFIG_DIR"); v != "" {
			return v, nil
		}
		h := getenv("HOME")
		if h == "" {
			return "", fmt.Errorf("no home")
		}
		return filepath.Join(h, ".claude"), nil
	}
	withEnv := func(k string) string {
		switch k {
		case "CLAUDE_CONFIG_DIR":
			return envRoot
		case "HOME":
			return homeDir
		default:
			return ""
		}
	}
	noEnv := func(k string) string {
		if k == "HOME" {
			return homeDir
		}
		return ""
	}

	calls = 0
	home, skip, err := resolveHome(protocol.HarnessClaude, &config.HarnessConfig{Enabled: true, Root: configRoot}, "", withEnv, adapter)
	if err != nil || skip || home != configRoot || calls != 0 {
		t.Fatalf("config root: home=%q skip=%v err=%v calls=%d", home, skip, err, calls)
	}

	home, skip, err = resolveHome(protocol.HarnessClaude, &config.HarnessConfig{Enabled: true}, "", withEnv, adapter)
	if err != nil || skip || home != envRoot {
		t.Fatalf("env: home=%q skip=%v err=%v", home, skip, err)
	}

	home, skip, err = resolveHome(protocol.HarnessClaude, nil, "", noEnv, adapter)
	if err != nil || skip || home != filepath.Join(homeDir, ".claude") {
		t.Fatalf("default: home=%q skip=%v err=%v", home, skip, err)
	}

	calls = 0
	home, skip, err = resolveHome(protocol.HarnessClaude, &config.HarnessConfig{Enabled: true, Root: configRoot}, flagRoot, withEnv, adapter)
	if err != nil || skip || home != flagRoot || calls != 0 {
		t.Fatalf("flag: home=%q skip=%v err=%v calls=%d", home, skip, err, calls)
	}

	home, skip, err = resolveHome(protocol.HarnessClaude, &config.HarnessConfig{Enabled: false, Root: configRoot}, "", withEnv, adapter)
	if err != nil || !skip || home != configRoot {
		t.Fatalf("disabled still resolves root: home=%q skip=%v err=%v", home, skip, err)
	}
}

func TestCursorConfigRootBeatsDefault(t *testing.T) {
	homeDir := t.TempDir()
	root := t.TempDir()
	getenv := func(k string) string {
		if k == "HOME" {
			return homeDir
		}
		return ""
	}
	def, err := cursor.Adapter{}.Home(getenv)
	if err != nil {
		t.Fatal(err)
	}
	home, skip, err := resolveHome(protocol.HarnessCursor, &config.HarnessConfig{Enabled: true, Root: root}, "", getenv, cursor.Adapter{}.Home)
	if err != nil || skip || home != root || home == def {
		t.Fatalf("config root %q default %q skip=%v err=%v", home, def, skip, err)
	}
	home, skip, err = resolveHome(protocol.HarnessCursor, nil, "", getenv, cursor.Adapter{}.Home)
	if err != nil || skip || home != def {
		t.Fatalf("default %q want %q err=%v", home, def, err)
	}
}

func TestOpenCodeEnvBeatsDefault(t *testing.T) {
	homeDir := t.TempDir()
	xdg := t.TempDir()
	getenv := func(k string) string {
		switch k {
		case "XDG_DATA_HOME":
			return xdg
		case "HOME":
			return homeDir
		default:
			return ""
		}
	}
	home, skip, err := resolveHome(protocol.HarnessOpenCode, nil, "", getenv, opencode.Adapter{}.Home)
	if err != nil || skip || home != filepath.Join(xdg, "opencode") {
		t.Fatalf("env home %q err=%v", home, err)
	}
	noEnv := func(k string) string {
		if k == "HOME" {
			return homeDir
		}
		return ""
	}
	home, skip, err = resolveHome(protocol.HarnessOpenCode, nil, "", noEnv, opencode.Adapter{}.Home)
	if err != nil || skip || home != filepath.Join(homeDir, ".local", "share", "opencode") {
		t.Fatalf("default home %q err=%v", home, err)
	}
}

func TestSourcesPartialRoot(t *testing.T) {
	f := newHarnessFixture(t)
	codexRoot := t.TempDir()
	// enabled omitted, the way a file that sets only root is written.
	raw := []byte(fmt.Sprintf("{\"codex\":{\"root\":%q}}", codexRoot))
	var loaded config.Harnesses
	if err := json.Unmarshal(raw, &loaded); err != nil {
		t.Fatal(err)
	}
	if !loaded.Enabled(protocol.HarnessCodex) || !loaded.Enabled(protocol.HarnessClaude) {
		t.Fatalf("enabled codex=%v claude=%v", loaded.Enabled(protocol.HarnessCodex), loaded.Enabled(protocol.HarnessClaude))
	}
	src, err := sources(f.getenv, loaded)
	if err != nil {
		t.Fatal(err)
	}
	got := sourceNames(src)
	if got[protocol.HarnessCodex] != codexRoot {
		t.Fatalf("codex home %q", got[protocol.HarnessCodex])
	}
	if got[protocol.HarnessClaude] != f.claude {
		t.Fatalf("claude home %q", got[protocol.HarnessClaude])
	}
	if got[protocol.HarnessTerva] != f.terva {
		t.Fatalf("terva home %q", got[protocol.HarnessTerva])
	}
}

func writeRel(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHarnessStatusesComplete(t *testing.T) {
	f := newHarnessFixture(t)
	rows := harnessStatuses(f.getenv, nil)
	ids := harnessIDs(rows)
	if strings.Join(ids, "\n") != strings.Join(knownIDs(), "\n") {
		t.Fatalf("order %v", ids)
	}
	if len(rows) != 6 {
		t.Fatalf("rows %d", len(rows))
	}
	for _, row := range rows {
		if !row.enabled || row.root == "" || row.source == "" {
			t.Fatalf("row %+v", row)
		}
		if !strings.Contains(row.line(), "enabled=true") || !strings.Contains(row.line(), "source="+row.source) {
			t.Fatalf("line %q", row.line())
		}
	}
	explicit := indexHarness(harnessStatuses(f.getenv, config.Harnesses{
		protocol.HarnessClaude: {Enabled: true},
	}))
	base := indexHarness(rows)
	if explicit[protocol.HarnessClaude] != base[protocol.HarnessClaude] {
		t.Fatalf("explicit enable %+v baseline %+v", explicit[protocol.HarnessClaude], base[protocol.HarnessClaude])
	}
}

func TestHarnessStatusesDisabledStillListed(t *testing.T) {
	f := newHarnessFixture(t)
	offMap := config.Harnesses{
		protocol.HarnessClaude: {Enabled: false},
	}
	base := indexHarness(harnessStatuses(f.getenv, nil))
	got := indexHarness(harnessStatuses(f.getenv, offMap))
	claude := got[protocol.HarnessClaude]
	if claude.enabled || claude.root == "" || claude.source == "" {
		t.Fatalf("disabled claude %+v", claude)
	}
	if claude.root != f.claude || claude.source != "env" {
		t.Fatalf("disabled claude root/source %+v", claude)
	}
	if claude.line() != fmt.Sprintf("harness claude enabled=false root=%s source=env", f.claude) {
		t.Fatalf("line %q", claude.line())
	}
	for _, id := range knownIDs() {
		if id == protocol.HarnessClaude {
			continue
		}
		if got[id] != base[id] {
			t.Fatalf("%s changed: got %+v base %+v", id, got[id], base[id])
		}
	}
	src, err := sources(f.getenv, offMap)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sourceNames(src)[protocol.HarnessClaude]; ok {
		t.Fatal("sources() included disabled claude")
	}
}

func TestHarnessStatusSourcePrecedence(t *testing.T) {
	homeDir := t.TempDir()
	configRoot := t.TempDir()
	envRoot := t.TempDir()
	zotRoot := t.TempDir()
	xdg := t.TempDir()

	with := func(extra func(string) string) func(string) string {
		return func(k string) string {
			if v := extra(k); v != "" {
				return v
			}
			if k == "HOME" {
				return homeDir
			}
			return ""
		}
	}

	claudeEnv := with(func(k string) string {
		if k == "CLAUDE_CONFIG_DIR" {
			return envRoot
		}
		return ""
	})
	both := indexHarness(harnessStatuses(claudeEnv, config.Harnesses{
		protocol.HarnessClaude: {Enabled: true, Root: configRoot},
	}))
	if both[protocol.HarnessClaude].source != "config" || both[protocol.HarnessClaude].root != configRoot {
		t.Fatalf("config beats env: %+v", both[protocol.HarnessClaude])
	}
	envOnly := indexHarness(harnessStatuses(claudeEnv, nil))
	if envOnly[protocol.HarnessClaude].source != "env" || envOnly[protocol.HarnessClaude].root != envRoot {
		t.Fatalf("env: %+v", envOnly[protocol.HarnessClaude])
	}
	neither := with(func(string) string { return "" })
	def := indexHarness(harnessStatuses(neither, nil))
	if def[protocol.HarnessClaude].source != "default" || def[protocol.HarnessClaude].root != filepath.Join(homeDir, ".claude") {
		t.Fatalf("default: %+v", def[protocol.HarnessClaude])
	}

	tervaEnv := with(func(k string) string {
		if k == "ZOT_HOME" {
			return zotRoot
		}
		return ""
	})
	terva := indexHarness(harnessStatuses(tervaEnv, nil))
	if terva[protocol.HarnessTerva].source != "env" || terva[protocol.HarnessTerva].root != zotRoot {
		t.Fatalf("zot env: %+v", terva[protocol.HarnessTerva])
	}
	tervaCfg := indexHarness(harnessStatuses(tervaEnv, config.Harnesses{
		protocol.HarnessTerva: {Enabled: true, Root: configRoot},
	}))
	if tervaCfg[protocol.HarnessTerva].source != "config" || tervaCfg[protocol.HarnessTerva].root != configRoot {
		t.Fatalf("terva config: %+v", tervaCfg[protocol.HarnessTerva])
	}

	cursorEnv := with(func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			return xdg
		}
		return ""
	})
	cursorHome, err := cursor.Adapter{}.Home(cursorEnv)
	if err != nil {
		t.Fatal(err)
	}
	cursorRows := indexHarness(harnessStatuses(cursorEnv, nil))
	if cursorRows[protocol.HarnessCursor].source != "default" || cursorRows[protocol.HarnessCursor].root != cursorHome {
		t.Fatalf("cursor xdg is default: %+v home %q", cursorRows[protocol.HarnessCursor], cursorHome)
	}
	cursorCfg := indexHarness(harnessStatuses(cursorEnv, config.Harnesses{
		protocol.HarnessCursor: {Enabled: true, Root: configRoot},
	}))
	if cursorCfg[protocol.HarnessCursor].source != "config" || cursorCfg[protocol.HarnessCursor].root != configRoot {
		t.Fatalf("cursor config: %+v", cursorCfg[protocol.HarnessCursor])
	}

	cliEnv := with(func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			return xdg
		}
		return ""
	})
	cliHome, err := cursorcli.Adapter{}.Home(cliEnv)
	if err != nil {
		t.Fatal(err)
	}
	cliRows := indexHarness(harnessStatuses(cliEnv, nil))
	if cliRows[protocol.HarnessCursorCLI].source != "default" || cliRows[protocol.HarnessCursorCLI].root != cliHome {
		t.Fatalf("cursor-cli xdg is default: %+v home %q", cliRows[protocol.HarnessCursorCLI], cliHome)
	}
	cliOverride := t.TempDir()
	cliSet := with(func(k string) string {
		switch k {
		case "XDG_CONFIG_HOME":
			return xdg
		case "CURSOR_CONFIG_DIR":
			return cliOverride
		default:
			return ""
		}
	})
	cliOn := indexHarness(harnessStatuses(cliSet, nil))
	if cliOn[protocol.HarnessCursorCLI].source != "env" || cliOn[protocol.HarnessCursorCLI].root != cliOverride {
		t.Fatalf("cursor-cli env: %+v", cliOn[protocol.HarnessCursorCLI])
	}
}

func TestHarnessStatusEmptyRoot(t *testing.T) {
	rows := harnessStatuses(func(string) string { return "" }, nil)
	if len(rows) != len(knownIDs()) {
		t.Fatalf("rows %d", len(rows))
	}
	for _, row := range rows {
		if row.root != "" || row.source != "default" || !row.enabled {
			t.Fatalf("unresolved %+v", row)
		}
		if row.line() != fmt.Sprintf("harness %s enabled=true root= source=default", row.id) {
			t.Fatalf("line %q", row.line())
		}
	}

	envRoot := t.TempDir()
	getenv := func(k string) string {
		if k == "CLAUDE_CONFIG_DIR" {
			return envRoot
		}
		return ""
	}
	got := indexHarness(harnessStatuses(getenv, nil))
	claude := got[protocol.HarnessClaude]
	if claude.root != envRoot || claude.source != "env" || !claude.enabled {
		t.Fatalf("override without HOME: %+v", claude)
	}
	for _, id := range knownIDs() {
		if id == protocol.HarnessClaude {
			continue
		}
		if got[id].root != "" || got[id].source != "default" {
			t.Fatalf("%s should be empty default: %+v", id, got[id])
		}
	}
}

func knownIDs() []string {
	src := knownSources()
	ids := make([]string, len(src))
	for i, s := range src {
		ids[i] = s.harness.Name()
	}
	return ids
}

func harnessIDs(rows []harnessStatus) []string {
	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.id
	}
	return ids
}

func indexHarness(rows []harnessStatus) map[string]harnessStatus {
	out := make(map[string]harnessStatus, len(rows))
	for _, row := range rows {
		out[row.id] = row
	}
	return out
}

func watchersAt(ws []*watch.Watcher, root string) int {
	n := 0
	for _, w := range ws {
		if w.Root == root {
			n++
		}
	}
	return n
}
