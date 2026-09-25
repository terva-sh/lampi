package discover

import (
	"io/fs"
	"path"
	"path/filepath"
	"strings"
)

// Classify reports whether a slash path relative to a terva home is an
// artifact. Sessions may be nested. Raati records and task boards are
// one directory deep, and a missing tree is simply absent from the walk.
func Classify(rel string) (string, bool) {
	rel = path.Clean(rel)
	if rel == "." {
		return "", false
	}
	name := path.Base(rel)
	if strings.HasPrefix(name, ".") {
		return "", false
	}
	switch {
	case strings.HasPrefix(rel, "sessions/"):
		if !strings.HasSuffix(name, ".jsonl") {
			return "", false
		}
		if strings.HasSuffix(name, ".errors.jsonl") {
			return KindErrors, true
		}
		return KindTranscript, true
	case raatiFile(rel):
		return KindRaati, true
	case tasksFile(rel):
		return KindTasks, true
	default:
		return "", false
	}
}

// Sidecars walks the optional raati and tasks trees. A missing directory
// is an empty list. Sessions are not included; call Sessions for those.
func Sidecars(tervaHome string) ([]File, error) {
	files, _, err := SidecarsSkipped(tervaHome)
	return files, err
}

// SidecarsSkipped is Sidecars plus the entries it left out.
func SidecarsSkipped(tervaHome string) ([]File, []error, error) {
	dirs := []string{
		filepath.Join(tervaHome, "raati"),
		filepath.Join(tervaHome, "tasks"),
		filepath.Join(tervaHome, "ext-data", "tasks"),
	}
	var out []File
	var skipped []error
	for _, dir := range dirs {
		files, skip, err := walkSidecarDir(tervaHome, dir)
		skipped = append(skipped, skip...)
		if err != nil {
			return nil, skipped, err
		}
		out = append(out, files...)
	}
	return out, skipped, nil
}

// TasksSessionID returns the session id embedded in a tasks archive name.
// ok is false when rel is not a tasks file. The id is the transcript
// meta id terva writes into tasks-<id>.json. An unsafe id is the 16-hex
// prefix of sha256(id), which is how terva names that file.
func TasksSessionID(rel string) (string, bool) {
	if !tasksFile(rel) {
		return "", false
	}
	name := path.Base(path.Clean(rel))
	id := strings.TrimSuffix(strings.TrimPrefix(name, "tasks-"), ".json")
	return id, true
}

func walkSidecarDir(home, dir string) ([]File, []error, error) {
	var out []File
	skipped, err := WalkFiles(dir, func(p string, d fs.DirEntry) error {
		rel, err := filepath.Rel(home, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		kind, ok := Classify(rel)
		if !ok || kind == KindTranscript || kind == KindErrors {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, File{
			AbsPath: p,
			RelPath: rel,
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

func raatiFile(rel string) bool {
	if path.Dir(rel) != "raati" {
		return false
	}
	name := path.Base(rel)
	rest, ok := strings.CutPrefix(name, "raati-")
	if !ok || !strings.HasSuffix(rest, ".json") {
		return false
	}
	digits := strings.TrimSuffix(rest, ".json")
	if digits == "" {
		return false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func tasksFile(rel string) bool {
	switch path.Dir(rel) {
	case "tasks", "ext-data/tasks":
	default:
		return false
	}
	name := path.Base(rel)
	rest, ok := strings.CutPrefix(name, "tasks-")
	if !ok || !strings.HasSuffix(rest, ".json") {
		return false
	}
	return safeSessionFileID(strings.TrimSuffix(rest, ".json"))
}

// safeSessionFileID matches terva's sessionFileName: a non-empty run of
// [A-Za-z0-9._-] up to 128 characters, without "..", and not "." or "..".
// Anything else is hashed into a 16-hex name before it is written, and
// that hex form is itself a safe id.
func safeSessionFileID(id string) bool {
	if id == "" || id == "." || id == ".." || len(id) > 128 || strings.Contains(id, "..") {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}
