//go:build synthetic_container

package container

import (
	"strings"
	"testing"

	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/testharness"
)

type plantFunc func(root, cwd string, sessions []testharness.SessionSpec) (testharness.PlantResult, error)

type plantedSession struct {
	harness string
	id      string
	plant   plantFunc
}

func fourPlants() []plantedSession {
	return []plantedSession{
		{protocol.HarnessTerva, "syn-terva-1", testharness.PlantTerva},
		{protocol.HarnessClaude, "syn-claude-1", testharness.PlantClaude},
		{protocol.HarnessCodex, "syn-codex-1", testharness.PlantCodex},
		{protocol.HarnessOpenCode, "syn-opencode-1", testharness.PlantOpenCode},
	}
}

func promptFor(id string) string {
	return "prompt " + id
}

func plantOne(t *testing.T, b *box, harness, cwd, id string, plant plantFunc) {
	t.Helper()
	res, err := plant(b.roots[harness], cwd, []testharness.SessionSpec{{
		ID:     id,
		Prompt: promptFor(id),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) == 0 {
		t.Fatalf("plant %s wrote no files", harness)
	}
}

func TestMultiHarnessStatus(t *testing.T) {
	b := openBox(t, "/work")
	stdout, stderr, err := b.lampi("status")
	if err != nil {
		t.Fatalf("status: %v\n%s", err, stderr)
	}
	if stderr != "" {
		t.Fatalf("status stderr: %s", stderr)
	}
	var want []string
	for _, row := range b.rows {
		want = append(want, row.line())
	}
	got := harnessLines(stdout)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("harness lines:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !strings.Contains(stdout, "health: ok") || !strings.Contains(stdout, "catalog_sessions: 0") {
		t.Fatalf("status:\n%s", stdout)
	}
}

func TestIngestExportFourHarnesses(t *testing.T) {
	b := openBox(t, "/work")
	const cwd = "/work/demo"
	var prompts []string
	for _, p := range fourPlants() {
		plantOne(t, b, p.harness, cwd, p.id, p.plant)
		prompts = append(prompts, promptFor(p.id))
	}

	stdout, stderr, err := b.lampi("sync")
	if err != nil {
		t.Fatalf("sync: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	first := parseSync(t, stdout)
	if first.uploaded != 4 || first.manifests != 4 || first.refused != 0 || first.quarantined != 0 {
		t.Fatalf("first sync: %+v\n%s", first, stdout)
	}
	before := casFiles(t, b.data)

	stdout, stderr, err = b.lampi("sync")
	if err != nil {
		t.Fatalf("resync: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	second := parseSync(t, stdout)
	if second.uploaded != 0 || second.missing != 0 {
		t.Fatalf("resync: %+v\n%s", second, stdout)
	}
	if got := casFiles(t, b.data); got != before {
		t.Fatalf("resync stored a blob: %d to %d", before, got)
	}

	events := b.exportEvents(t)
	for _, prompt := range prompts {
		if !strings.Contains(events, prompt) {
			t.Fatalf("export missing %q\n%s", prompt, events)
		}
	}
}

func TestAllowlistRefuse(t *testing.T) {
	requireRuntime(t)
	t.Run("empty", func(t *testing.T) {
		b := openBox(t, "")
		plantOne(t, b, protocol.HarnessTerva, "/work/demo", "syn-terva-1", testharness.PlantTerva)
		assertRefused(t, b, "")
	})
	t.Run("outside", func(t *testing.T) {
		b := openBox(t, "/work")
		plantOne(t, b, protocol.HarnessTerva, "/outside", "syn-terva-1", testharness.PlantTerva)
		assertRefused(t, b, "/outside")
	})
}

func assertRefused(t *testing.T, b *box, cwd string) {
	t.Helper()
	stdout, stderr, err := b.lampi("sync")
	if err == nil || !strings.Contains(stderr, "not allowlisted") {
		t.Fatalf("err %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if cwd != "" && !strings.Contains(stderr, cwd) {
		t.Fatalf("err %v\nstderr:\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "refused") {
		t.Fatalf("stdout: %s", stdout)
	}
	c := parseSync(t, stdout)
	if c.uploaded != 0 || c.manifests != 0 || c.refused < 1 {
		t.Fatalf("sync: %+v\n%s", c, stdout)
	}
	if n := casFiles(t, b.data); n != 0 {
		t.Fatalf("cas files %d", n)
	}
}

func TestDisabledHarnessNotIngested(t *testing.T) {
	b := openBox(t, "/work", protocol.HarnessClaude)
	stdout, stderr, err := b.lampi("status")
	if err != nil {
		t.Fatalf("status: %v\n%s", err, stderr)
	}
	var want []string
	for _, row := range b.rows {
		want = append(want, row.line())
	}
	got := harnessLines(stdout)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("harness lines:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	const cwd = "/work/demo"
	plantOne(t, b, protocol.HarnessTerva, cwd, "syn-terva-1", testharness.PlantTerva)
	plantOne(t, b, protocol.HarnessClaude, cwd, "syn-claude-1", testharness.PlantClaude)

	stdout, stderr, err = b.lampi("sync")
	if err != nil {
		t.Fatalf("sync: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	c := parseSync(t, stdout)
	if c.uploaded != 1 || c.manifests != 1 || c.refused != 0 || c.quarantined != 0 {
		t.Fatalf("sync: %+v\n%s", c, stdout)
	}
	events := b.exportEvents(t)
	if !strings.Contains(events, promptFor("syn-terva-1")) {
		t.Fatalf("export missing terva prompt\n%s", events)
	}
	if strings.Contains(events, promptFor("syn-claude-1")) {
		t.Fatalf("export ingested disabled claude\n%s", events)
	}
}
