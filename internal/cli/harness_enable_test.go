package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/auth"
	"terva.sh/lampi/internal/outbox"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/watermark"
)

func TestDisabledHarnessLeavesWatermarkAndCAS(t *testing.T) {
	data := t.TempDir()
	lake, err := api.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	tok := "abc123"
	if err := auth.Write(filepath.Join(data, "token"), tok); err != nil {
		t.Fatal(err)
	}
	lake.Allow(tok)
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)

	tervaHome := t.TempDir()
	claudeHome := t.TempDir()
	cfg := t.TempDir()
	state := t.TempDir()
	tervaBody := []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"sess-1\",\"cwd\":\"/work/app\"}}\n")
	claudeBody := []byte("{\"type\":\"user\",\"sessionId\":\"sid-1\",\"cwd\":\"/work/app\"}\n")
	extra := []byte("{\"type\":\"user\",\"sessionId\":\"sid-1\",\"cwd\":\"/work/app\",\"n\":2}\n")
	writeRel(t, tervaHome, "sessions/abcd/sess-1.jsonl", string(tervaBody))
	claudeRel := "projects/-work-app/sid-1.jsonl"
	writeRel(t, claudeHome, claudeRel, string(claudeBody))
	tokenCopy := filepath.Join(cfg, "token")
	if err := auth.Write(tokenCopy, tok); err != nil {
		t.Fatal(err)
	}
	writeClientConfig(t, cfg, "")

	env := Env{Getenv: func(k string) string {
		switch k {
		case "TERVA_HOME":
			return tervaHome
		case "CLAUDE_CONFIG_DIR":
			return claudeHome
		case "XDG_CONFIG_HOME":
			return cfg
		case "XDG_STATE_HOME":
			return state
		default:
			return ""
		}
	}}
	args := []string{"sync", "--server", srv.URL, "--token-file", tokenCopy}
	out := runCaptured(t, args, env)
	if !strings.Contains(out, "uploaded 2") {
		t.Fatalf("first sync: %s", out)
	}

	stateDir := filepath.Join(state, "terva-lampi")
	machine := readMachineID(t, cfg)
	tervaMark := readCursor(t, watermark.File(stateDir), machine, protocol.HarnessTerva, tervaHome, "sessions/abcd/sess-1.jsonl")
	claudeMark := readCursor(t, watermark.File(stateDir), machine, protocol.HarnessClaude, claudeHome, claudeRel)
	if tervaMark.offset != int64(len(tervaBody)) || claudeMark.offset != int64(len(claudeBody)) {
		t.Fatalf("cursors terva=%d claude=%d", tervaMark.offset, claudeMark.offset)
	}
	if rows := cursorCount(t, watermark.File(stateDir)); rows != 2 {
		t.Fatalf("watermark rows %d", rows)
	}
	if n := outboxRows(t, outbox.File(stateDir)); n != 0 {
		t.Fatalf("outbox rows %d", n)
	}
	casBefore := casSnapshot(t, filepath.Join(data, "cas"))
	origSHA := claudeMark.sum
	if ok, err := lake.CAS.Has(origSHA); err != nil || !ok {
		t.Fatalf("original blob missing ok=%v err=%v", ok, err)
	}

	writeRel(t, claudeHome, claudeRel, string(append(append([]byte(nil), claudeBody...), extra...)))
	tailSHA := sha256Hex(extra)
	fullSHA := sha256Hex(append(append([]byte(nil), claudeBody...), extra...))
	writeClientConfig(t, cfg, `{"claude":{"enabled":false}}`)

	var discovered bytes.Buffer
	disc := env
	disc.Stdout = &discovered
	disc.Stderr = &bytes.Buffer{}
	if err := Run([]string{"agent", "discover"}, disc); err != nil {
		t.Fatal(err)
	}
	text := discovered.String()
	if strings.Contains(text, "claude\t") {
		t.Fatalf("discover listed disabled claude:\n%s", text)
	}
	if !strings.Contains(text, "terva\tsessions/abcd/sess-1.jsonl") {
		t.Fatalf("discover lost terva:\n%s", text)
	}

	out = runCaptured(t, args, env)
	if !strings.Contains(out, "uploaded 0") || !strings.Contains(out, "checked 1") {
		t.Fatalf("disabled sync: %s", out)
	}
	if got := readCursor(t, watermark.File(stateDir), machine, protocol.HarnessClaude, claudeHome, claudeRel); got != claudeMark {
		t.Fatalf("claude watermark changed\n before %+v\n after  %+v", claudeMark, got)
	}
	// Terva stays enabled, so this sync may refresh its row. The cursor
	// itself has to stay. Claude, above, must not even be rewritten.
	if got := readCursor(t, watermark.File(stateDir), machine, protocol.HarnessTerva, tervaHome, "sessions/abcd/sess-1.jsonl"); !sameCursor(got, tervaMark) {
		t.Fatalf("terva cursor changed\n before %+v\n after  %+v", tervaMark, got)
	}
	if rows := cursorCount(t, watermark.File(stateDir)); rows != 2 {
		t.Fatalf("watermark rows %d", rows)
	}
	if n := outboxRows(t, outbox.File(stateDir)); n != 0 {
		t.Fatalf("outbox rows %d", n)
	}
	casAfter := casSnapshot(t, filepath.Join(data, "cas"))
	if !sameSnapshot(casBefore, casAfter) {
		t.Fatalf("CAS changed while claude was disabled\n before %v\n after  %v", casBefore, casAfter)
	}
	if ok, err := lake.CAS.Has(tailSHA); err != nil || ok {
		t.Fatalf("tail blob stored while disabled ok=%v err=%v", ok, err)
	}
	if ok, err := lake.CAS.Has(fullSHA); err != nil || ok {
		t.Fatalf("grown blob stored while disabled ok=%v err=%v", ok, err)
	}

	writeClientConfig(t, cfg, `{"claude":{"enabled":true}}`)
	out = runCaptured(t, args, env)
	if !strings.Contains(out, "uploaded 1") {
		t.Fatalf("resume sync: %s", out)
	}
	resumed := readCursor(t, watermark.File(stateDir), machine, protocol.HarnessClaude, claudeHome, claudeRel)
	if resumed.offset != int64(len(claudeBody)+len(extra)) || resumed.sum == claudeMark.sum {
		t.Fatalf("resume cursor %+v", resumed)
	}
	if got := readCursor(t, watermark.File(stateDir), machine, protocol.HarnessTerva, tervaHome, "sessions/abcd/sess-1.jsonl"); !sameCursor(got, tervaMark) {
		t.Fatalf("terva cursor moved on claude resume: %+v", got)
	}
	if ok, err := lake.CAS.Has(origSHA); err != nil || !ok {
		t.Fatalf("original blob gone ok=%v err=%v", ok, err)
	}
	if ok, err := lake.CAS.Has(tailSHA); err != nil || !ok {
		t.Fatalf("tail blob missing ok=%v err=%v", ok, err)
	}
	if ok, err := lake.CAS.Has(fullSHA); err != nil || !ok {
		t.Fatalf("assembled blob missing ok=%v err=%v", ok, err)
	}
	_, arts, ok, err := lake.Catalog.Current(context.Background(), protocol.HarnessClaude, "sid-1")
	if err != nil || !ok || len(arts) != 1 {
		t.Fatalf("catalog ok=%v err=%v arts=%d", ok, err, len(arts))
	}
	if arts[0].Relation != protocol.RelationGrownFrom || arts[0].GrownFrom != origSHA || arts[0].SHA256 != fullSHA {
		t.Fatalf("resume was not a tail: %+v", arts[0])
	}
	if _, arts, ok, err := lake.Catalog.Current(context.Background(), protocol.HarnessTerva, "sess-1"); err != nil || !ok || len(arts) != 1 || arts[0].SHA256 != tervaMark.sum {
		t.Fatalf("terva catalog ok=%v err=%v arts=%v", ok, err, arts)
	}
}

func runCaptured(t *testing.T, args []string, env Env) string {
	t.Helper()
	var out, errb bytes.Buffer
	env.Stdout = &out
	env.Stderr = &errb
	if err := Run(args, env); err != nil {
		t.Fatalf("%v\n%s\n%s", err, out.String(), errb.String())
	}
	return out.String()
}

func writeClientConfig(t *testing.T, cfg, harnesses string) {
	t.Helper()
	dir := filepath.Join(cfg, "terva-lampi")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	var body map[string]json.RawMessage
	projects := []byte(`{"allow":[{"cwd_prefix":"/work/app"}]}`)
	body = map[string]json.RawMessage{"projects": projects}
	if harnesses != "" {
		body["harnesses"] = json.RawMessage(harnesses)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(filepath.Join(dir, "config.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

type cursorRow struct {
	size    int64
	offset  int64
	sum     string
	updated string
}

func sameCursor(a, b cursorRow) bool {
	return a.size == b.size && a.offset == b.offset && a.sum == b.sum
}

func readCursor(t *testing.T, dbPath, machine, harness, root, rel string) cursorRow {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var row cursorRow
	err = db.QueryRow(`
		SELECT size, sha256, byte_offset, updated_at
		FROM watermarks
		WHERE machine_id = ? AND harness = ? AND root_path = ? AND relative_path = ?`,
		machine, harness, root, rel,
	).Scan(&row.size, &row.sum, &row.offset, &row.updated)
	if err != nil {
		t.Fatalf("watermark %s %s: %v", harness, rel, err)
	}
	return row
}

func cursorCount(t *testing.T, dbPath string) int {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM watermarks`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func outboxRows(t *testing.T, dbPath string) int {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM items`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func casSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = sha256Hex(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameSnapshot(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
