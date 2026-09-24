package testharness_test

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"terva.sh/lampi/internal/testharness"
)

func TestPlantLayouts(t *testing.T) {
	cwd := "/work/demo"
	prompt := `pond <sample> & more`
	id := "sess-1"

	t.Run("terva", func(t *testing.T) {
		root := t.TempDir()
		res := plantTerva(t, root, cwd, []testharness.SessionSpec{{
			ID: id, Prompt: prompt,
		}})
		if got := relSlash(t, root, res.Files[0]); got != "sessions/sess-1/sess-1.jsonl" {
			t.Fatalf("path %s", got)
		}
		if res.SessionIDs[0] != id {
			t.Fatalf("id %s", res.SessionIDs[0])
		}
		body := read(t, res.Files[0])
		assertCleartext(t, body, id, cwd, prompt)
		lines := jsonLines(t, body)
		if len(lines) != 2 {
			t.Fatalf("lines %d", len(lines))
		}
		var meta struct {
			Type string `json:"type"`
			Meta struct {
				ID  string `json:"id"`
				CWD string `json:"cwd"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(lines[0], &meta); err != nil {
			t.Fatal(err)
		}
		if meta.Type != "meta" || meta.Meta.ID != id || meta.Meta.CWD != cwd {
			t.Fatalf("meta %+v", meta)
		}
		var msg struct {
			Type    string `json:"type"`
			Message struct {
				Role    string `json:"role"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(lines[1], &msg); err != nil {
			t.Fatal(err)
		}
		if msg.Type != "message" || msg.Message.Role != "user" || len(msg.Message.Content) != 1 || msg.Message.Content[0].Type != "text" || msg.Message.Content[0].Text != prompt {
			t.Fatalf("message %+v", msg)
		}
		if strings.Contains(body, ".errors.jsonl") {
			t.Fatalf("errors sidecar name in body: %s", body)
		}
	})

	t.Run("claude", func(t *testing.T) {
		root := t.TempDir()
		res := plantClaude(t, root, cwd, []testharness.SessionSpec{{
			ID: id, Prompt: prompt,
		}})
		if got := relSlash(t, root, res.Files[0]); got != "projects/-work-demo/sess-1.jsonl" {
			t.Fatalf("path %s", got)
		}
		body := read(t, res.Files[0])
		assertCleartext(t, body, id, cwd, prompt)
		lines := jsonLines(t, body)
		if len(lines) != 1 {
			t.Fatalf("lines %d", len(lines))
		}
		var line struct {
			Type      string `json:"type"`
			SessionID string `json:"sessionId"`
			CWD       string `json:"cwd"`
			Message   struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(lines[0], &line); err != nil {
			t.Fatal(err)
		}
		if line.Type != "user" || line.SessionID != id || line.CWD != cwd || line.Message.Role != "user" || line.Message.Content != prompt {
			t.Fatalf("line %+v", line)
		}
	})

	t.Run("claude deeper cwd", func(t *testing.T) {
		root := t.TempDir()
		deep := "/home/drew/src/foo"
		res := plantClaude(t, root, deep, []testharness.SessionSpec{{
			ID: id, Prompt: prompt,
		}})
		if got := relSlash(t, root, res.Files[0]); got != "projects/-home-drew-src-foo/sess-1.jsonl" {
			t.Fatalf("path %s", got)
		}
		assertCleartext(t, read(t, res.Files[0]), id, deep, prompt)
	})

	t.Run("codex", func(t *testing.T) {
		root := t.TempDir()
		res := plantCodex(t, root, cwd, []testharness.SessionSpec{{
			ID: id, Prompt: prompt,
		}})
		got := relSlash(t, root, res.Files[0])
		re := regexp.MustCompile(`^sessions/\d{4}/\d{2}/\d{2}/rollout-\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}-sess-1\.jsonl$`)
		if !re.MatchString(got) {
			t.Fatalf("path %s", got)
		}
		if strings.HasSuffix(got, "history.jsonl") || strings.Contains(got, "history.jsonl") {
			t.Fatalf("history path %s", got)
		}
		body := read(t, res.Files[0])
		assertCleartext(t, body, id, cwd, prompt)
		lines := jsonLines(t, body)
		if len(lines) != 2 {
			t.Fatalf("lines %d", len(lines))
		}
		var meta struct {
			Type    string `json:"type"`
			Payload struct {
				ID  string `json:"id"`
				CWD string `json:"cwd"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(lines[0], &meta); err != nil {
			t.Fatal(err)
		}
		if meta.Type != "session_meta" || meta.Payload.ID != id || meta.Payload.CWD != cwd {
			t.Fatalf("meta %+v", meta)
		}
		var user struct {
			Type    string `json:"type"`
			Payload struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(lines[1], &user); err != nil {
			t.Fatal(err)
		}
		if user.Type != "event_msg" || user.Payload.Type != "user_message" || user.Payload.Message != prompt {
			t.Fatalf("user %+v", user)
		}
		if _, err := os.Stat(filepath.Join(root, "history.jsonl")); !os.IsNotExist(err) {
			t.Fatalf("history.jsonl at home: %v", err)
		}
	})

	t.Run("opencode", func(t *testing.T) {
		root := t.TempDir()
		res := plantOpenCode(t, root, cwd, []testharness.SessionSpec{{
			ID: id, Prompt: prompt,
		}})
		if got := relSlash(t, root, res.Files[0]); got != "export/sess-1.json" {
			t.Fatalf("path %s", got)
		}
		body := read(t, res.Files[0])
		assertCleartext(t, body, id, cwd, prompt)
		var doc struct {
			Info struct {
				ID        string `json:"id"`
				Directory string `json:"directory"`
			} `json:"info"`
			Messages []struct {
				Info struct {
					Role string `json:"role"`
				} `json:"info"`
				Parts []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"messages"`
		}
		if err := json.Unmarshal([]byte(body), &doc); err != nil {
			t.Fatal(err)
		}
		if doc.Info.ID != id || doc.Info.Directory != cwd || len(doc.Messages) != 1 || doc.Messages[0].Info.Role != "user" {
			t.Fatalf("doc %+v", doc)
		}
		parts := doc.Messages[0].Parts
		if len(parts) != 1 || parts[0].Type != "text" || parts[0].Text != prompt {
			t.Fatalf("parts %+v", parts)
		}
		if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(d.Name(), ".db") {
				t.Fatalf("planted database %s", path)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestPlantDefaultsAndExtra(t *testing.T) {
	root := t.TempDir()
	cwd := "/work/demo"
	res := plantTerva(t, root, cwd, []testharness.SessionSpec{
		{Extra: map[string]string{"leak": "should-not-appear-xyz"}},
		{Prompt: " "},
	})
	if len(res.Files) != 2 || len(res.SessionIDs) != 2 {
		t.Fatalf("result %+v", res)
	}
	if res.SessionIDs[0] == res.SessionIDs[1] {
		t.Fatalf("ids collided: %s", res.SessionIDs[0])
	}
	for _, id := range res.SessionIDs {
		if !strings.HasPrefix(id, "syn-") {
			t.Fatalf("id %s", id)
		}
	}
	body := read(t, res.Files[0])
	wantPrompt := "synthetic prompt " + res.SessionIDs[0]
	assertCleartext(t, body, res.SessionIDs[0], cwd, wantPrompt)
	if strings.Contains(body, "should-not-appear-xyz") || strings.Contains(body, "leak") {
		t.Fatalf("extra key was written: %s", body)
	}
	spaced := read(t, res.Files[1])
	if !strings.Contains(spaced, `"text":" "`) && !strings.Contains(spaced, `"text": " "`) {
		t.Fatalf("whitespace prompt was replaced: %s", spaced)
	}
}

func TestPlantIdempotent(t *testing.T) {
	cwd := "/work/demo"

	t.Run("terva", func(t *testing.T) {
		root := t.TempDir()
		first := plantTerva(t, root, cwd, []testharness.SessionSpec{
			{ID: "keep", Prompt: "stay"},
			{ID: "repl", Prompt: "first"},
		})
		sibling := read(t, first.Files[0])
		second := plantTerva(t, root, cwd, []testharness.SessionSpec{
			{ID: "repl", Prompt: "second"},
		})
		if second.Files[0] != first.Files[1] {
			t.Fatalf("path %s want %s", second.Files[0], first.Files[1])
		}
		if got := read(t, first.Files[0]); got != sibling {
			t.Fatalf("sibling changed: %s", got)
		}
		body := read(t, second.Files[0])
		if strings.Contains(body, "first") || !strings.Contains(body, "second") {
			t.Fatalf("overwrite: %s", body)
		}
		n := 0
		filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(d.Name(), ".jsonl") {
				n++
			}
			return nil
		})
		if n != 2 {
			t.Fatalf("jsonl files %d", n)
		}
	})

	t.Run("codex finds existing rollout", func(t *testing.T) {
		root := t.TempDir()
		first := plantCodex(t, root, cwd, []testharness.SessionSpec{
			{ID: "thread-1", Prompt: "first"},
		})
		renamed := filepath.Join(root, "sessions", "2020", "01", "02", "rollout-2020-01-02T03-04-05-thread-1.jsonl")
		if err := os.MkdirAll(filepath.Dir(renamed), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(first.Files[0], renamed); err != nil {
			t.Fatal(err)
		}
		hist := filepath.Join(filepath.Dir(renamed), "history.jsonl")
		if err := os.WriteFile(hist, []byte("history-sentinel\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		second := plantCodex(t, root, cwd, []testharness.SessionSpec{
			{ID: "thread-1", Prompt: "second"},
		})
		if second.Files[0] != renamed {
			t.Fatalf("wrote %s want %s", second.Files[0], renamed)
		}
		body := read(t, renamed)
		assertCleartext(t, body, "thread-1", cwd, "second")
		if strings.Contains(body, "first") {
			t.Fatalf("old prompt kept: %s", body)
		}
		if got := read(t, hist); got != "history-sentinel\n" {
			t.Fatalf("history.jsonl changed: %s", got)
		}
		n := 0
		filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err == nil && strings.HasPrefix(d.Name(), "rollout-") {
				n++
			}
			return nil
		})
		if n != 1 {
			t.Fatalf("rollouts %d", n)
		}
	})
}

func TestPlantRejects(t *testing.T) {
	abs := t.TempDir()
	specs := []testharness.SessionSpec{{ID: "ok", Prompt: "hi"}}
	if _, err := testharness.PlantTerva("relative", "/work/demo", specs); err == nil || !strings.Contains(err.Error(), "root must be an absolute path") {
		t.Fatalf("root: %v", err)
	}
	if _, err := testharness.PlantClaude(abs, "work/demo", specs); err == nil || !strings.Contains(err.Error(), "cwd must be an absolute path") {
		t.Fatalf("cwd: %v", err)
	}
	for _, id := range []string{"a/b", `a\b`, ".hidden", "foo.errors", "..", "."} {
		root := t.TempDir()
		_, err := testharness.PlantOpenCode(root, "/work/demo", []testharness.SessionSpec{{ID: id}})
		if err == nil || !strings.Contains(err.Error(), "invalid session id") {
			t.Fatalf("id %q: %v", id, err)
		}
		left, walkErr := os.ReadDir(root)
		if walkErr != nil {
			t.Fatal(walkErr)
		}
		if len(left) != 0 {
			t.Fatalf("id %q wrote %d entries", id, len(left))
		}
	}

	root := t.TempDir()
	_, err := testharness.PlantTerva(root, "/work/demo", []testharness.SessionSpec{
		{ID: "ok", Prompt: "hi"},
		{ID: "bad/id", Prompt: "no"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	left, walkErr := os.ReadDir(root)
	if walkErr != nil {
		t.Fatal(walkErr)
	}
	if len(left) != 0 {
		t.Fatalf("partial plant wrote %d entries", len(left))
	}

	empty := plantCodex(t, abs, "/work/demo", nil)
	if len(empty.Files) != 0 || len(empty.SessionIDs) != 0 {
		t.Fatalf("empty %+v", empty)
	}
}

func TestImportBoundary(t *testing.T) {
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	entries, err := os.ReadDir(filepath.Dir(here))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var checked int
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(filepath.Dir(here), name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		for _, imp := range file.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if path == "terva.sh/lampi/internal/protocol" {
				continue
			}
			first, _, _ := strings.Cut(path, "/")
			if strings.Contains(first, ".") {
				t.Fatalf("%s imports %s", name, path)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no non-test go files")
	}
}

func plantTerva(t *testing.T, root, cwd string, specs []testharness.SessionSpec) testharness.PlantResult {
	t.Helper()
	res, err := testharness.PlantTerva(root, cwd, specs)
	return mustPlant(t, res, err)
}

func plantClaude(t *testing.T, root, cwd string, specs []testharness.SessionSpec) testharness.PlantResult {
	t.Helper()
	res, err := testharness.PlantClaude(root, cwd, specs)
	return mustPlant(t, res, err)
}

func plantCodex(t *testing.T, root, cwd string, specs []testharness.SessionSpec) testharness.PlantResult {
	t.Helper()
	res, err := testharness.PlantCodex(root, cwd, specs)
	return mustPlant(t, res, err)
}

func plantOpenCode(t *testing.T, root, cwd string, specs []testharness.SessionSpec) testharness.PlantResult {
	t.Helper()
	res, err := testharness.PlantOpenCode(root, cwd, specs)
	return mustPlant(t, res, err)
}

func mustPlant(t *testing.T, res testharness.PlantResult, err error) testharness.PlantResult {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range res.Files {
		if !filepath.IsAbs(path) {
			t.Fatalf("relative file %s", path)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.IsDir() || info.Size() == 0 {
			t.Fatalf("file %s size %d", path, info.Size())
		}
	}
	if len(res.Files) != len(res.SessionIDs) {
		t.Fatalf("files %d ids %d", len(res.Files), len(res.SessionIDs))
	}
	return res
}

func relSlash(t *testing.T, root, path string) string {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(rel)
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func assertCleartext(t *testing.T, body, id, cwd, prompt string) {
	t.Helper()
	for _, want := range []string{id, cwd, prompt} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
}

func jsonLines(t *testing.T, body string) [][]byte {
	t.Helper()
	if body == "" || !strings.HasSuffix(body, "\n") {
		t.Fatalf("body does not end with a newline: %q", body)
	}
	parts := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	out := make([][]byte, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			t.Fatal("blank jsonl line")
		}
		out = append(out, []byte(part))
	}
	return out
}
