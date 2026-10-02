package grok

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadSummaryTitleModelAndFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "summary.json")
	body := `{"info":{"cwd":"/work/demo","id":"018f1a2b-3c4d-7e5f-8a9b-0c1d2e3f4a5b"},"generated_title":"built","session_summary":"older","current_model_id":"grok-build","extra":1}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	ident, err := readSummary(path)
	if err != nil {
		t.Fatal(err)
	}
	if ident.CWD != "/work/demo" || ident.Title != "built" || ident.Model != "grok-build" {
		t.Fatalf("ident %+v", ident)
	}

	if err := os.WriteFile(path, []byte(`{"session_summary":"only summary","current_model_id":{"id":"nope"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ident, err = readSummary(path)
	if err != nil {
		t.Fatal(err)
	}
	if ident.Title != "only summary" || ident.Model != "" || ident.CWD != "" {
		t.Fatalf("fallback %+v", ident)
	}

	if err := os.WriteFile(path, []byte("not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	ident, err = readSummary(path)
	if err != nil || ident != (fileIdent{}) {
		t.Fatalf("bad json ident %+v err %v", ident, err)
	}
}
