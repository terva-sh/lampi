// Package discover finds terva session files on disk.
//
// It does not parse them. The terva adapter owns the meta line and
// decides which raati record or tasks archive belongs to a session.
// Claude Code and Codex keep their own trees under internal/adapter;
// they do not grow this package.
package discover

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// File is one JSONL object under a terva home.
type File struct {
	AbsPath string
	RelPath string
	Size    int64
	ModTime time.Time
	Kind    string
}

const (
	// KindTranscript is a session JSONL.
	KindTranscript = "transcript_jsonl"
	// KindErrors is a *.errors.jsonl sidecar.
	KindErrors = "errors_jsonl"
	// KindRaati is a deliberation record at raati/raati-<digits>.json.
	// The wire value matches protocol.KindRaatiJSON.
	KindRaati = "raati_json"
	// KindTasks is a task board at tasks/tasks-<id>.json, or the legacy
	// copy under ext-data/tasks/. The file holds archived generations.
	// The wire value matches protocol.KindTasksJSON.
	KindTasks = "tasks_json"
)

// TervaHome resolves the producer directory this machine's terva writes.
// TERVA_HOME wins, then the legacy ZOT_HOME name terva still honors, then
// the platform default from terva's own layout.
func TervaHome(getenv func(string) string) (string, error) {
	if v := getenv("TERVA_HOME"); v != "" {
		return v, nil
	}
	if v := getenv("ZOT_HOME"); v != "" {
		return v, nil
	}
	switch runtime.GOOS {
	case "darwin":
		home, err := home(getenv)
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "terva"), nil
	case "windows":
		if v := getenv("LOCALAPPDATA"); v != "" {
			return filepath.Join(v, "terva"), nil
		}
		home, err := home(getenv)
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "AppData", "Local", "terva"), nil
	default:
		if v := getenv("XDG_STATE_HOME"); v != "" {
			return filepath.Join(v, "terva"), nil
		}
		home, err := home(getenv)
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "state", "terva"), nil
	}
}

func home(getenv func(string) string) (string, error) {
	if h := getenv("HOME"); h != "" {
		return h, nil
	}
	if h := getenv("USERPROFILE"); h != "" {
		return h, nil
	}
	return "", fmt.Errorf("discover: HOME is not set")
}

// Sessions walks $TERVA_HOME/sessions for *.jsonl. A missing sessions
// directory is an empty list: this machine may not have run terva. An
// entry that cannot be read is left out; SessionsSkipped reports it.
func Sessions(tervaHome string) ([]File, error) {
	files, _, err := SessionsSkipped(tervaHome)
	return files, err
}

// SessionsSkipped is Sessions plus the entries it left out, each an
// error that names the path.
func SessionsSkipped(tervaHome string) ([]File, []error, error) {
	var out []File
	skipped, err := WalkFiles(filepath.Join(tervaHome, "sessions"), func(path string, d fs.DirEntry) error {
		name := d.Name()
		if strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(tervaHome, path)
		if err != nil {
			return err
		}
		kind := KindTranscript
		if strings.HasSuffix(name, ".errors.jsonl") {
			kind = KindErrors
		}
		out = append(out, File{
			AbsPath: path,
			RelPath: filepath.ToSlash(rel),
			Size:    info.Size(),
			ModTime: info.ModTime().UTC(),
			Kind:    kind,
		})
		return nil
	})
	if err != nil {
		return nil, skipped, err
	}
	return out, skipped, nil
}
