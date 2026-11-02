package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/catalog"
)

func TestQueryMetadataUsesReadTokenAndExplicitPaging(t *testing.T) {
	lake, srv, _ := mcpLake(t)
	token := tokenFile(t, lake, catalog.PermEventsRead)
	var out, stderr bytes.Buffer
	env := Env{Stdout: &out, Stderr: &stderr}
	for _, kind := range []string{"sessions", "projects", "bays"} {
		out.Reset()
		if err := Run([]string{"query", kind, "--server", srv.URL, "--token-file", token, "--limit", "1"}, env); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		var page struct {
			Items []json.RawMessage `json:"items"`
			Next  string            `json:"next_cursor"`
		}
		if json.Unmarshal(out.Bytes(), &page) != nil || page.Items == nil {
			t.Fatalf("bad %s page %s", kind, out.String())
		}
		if kind == "sessions" && len(page.Items) > 0 {
			var item struct {
				UID string `json:"session_uid"`
			}
			if err := json.Unmarshal(page.Items[0], &item); err != nil {
				t.Fatal(err)
			}
			out.Reset()
			path := filepath.Join(t.TempDir(), "session.json")
			if err := Run([]string{"query", "session", item.UID, "--server", srv.URL, "--token-file", token, "--out", path}, env); err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(path)
			if err != nil || !json.Valid(b) || !bytes.Contains(b, []byte(`"cwd_hash"`)) {
				t.Fatalf("session metadata %s %v", b, err)
			}
			if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0600 {
				t.Fatalf("mode %v %v", st, err)
			}
		}
	}
	out.Reset()
	if err := Run([]string{"query", "session", "unknown", "--server", srv.URL, "--token-file", token}, env); err == nil || out.Len() != 0 {
		t.Fatalf("unknown session produced output/error=%v", err)
	}
	for _, args := range [][]string{{"query", "session"}, {"query", "sessions", "extra"}, {"query", "session", "uid", "--limit", "1"}} {
		if err := Run(args, env); err == nil {
			t.Errorf("invalid arguments accepted %v", args)
		}
	}
	out.Reset()
	if err := Run([]string{"query", "sessions", "--help"}, env); err != nil || !strings.Contains(out.String(), "next_cursor") {
		t.Fatalf("metadata help: %v %s", err, out.String())
	}
}
