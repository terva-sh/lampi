package normalize

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// WriteJSONL writes one event per line. HTML escaping is off so a
// prompt that contains < or & is stored as itself. DuckDB reads the
// file with read_ndjson. sqlite reads each line and uses
// json_extract(line, '$.content_text').
func WriteJSONL(w io.Writer, events []Event) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for _, ev := range events {
		if err := enc.Encode(ev); err != nil {
			return fmt.Errorf("normalize: %w", err)
		}
	}
	return nil
}

// WriteFile writes events as JSONL at path, mode 0600. The write lands
// by rename, so a reader does not observe a partial file. path's
// directory is created mode 0700.
func WriteFile(path string, events []Event) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("normalize: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".events-*")
	if err != nil {
		return fmt.Errorf("normalize: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		if tmpName != "" {
			os.Remove(tmpName)
		}
	}()
	if err := WriteJSONL(tmp, events); err != nil {
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("normalize: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("normalize: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("normalize: %w", err)
	}
	tmpName = ""
	return nil
}
