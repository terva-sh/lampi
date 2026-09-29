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
	enc := lineEncoder(w)
	for _, ev := range events {
		if err := enc.Encode(ev); err != nil {
			return fmt.Errorf("normalize: %w", err)
		}
	}
	return nil
}

func lineEncoder(w io.Writer) *json.Encoder {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc
}

// WriteFile writes uid's events under dir as a compressed events file
// (see eventsfile.go), mode 0600, and removes a plain file an older
// release wrote. The write lands by rename, so a reader does not
// observe a partial file. dir is created mode 0700.
func WriteFile(dir, uid string, events []Event) error {
	path := EventsPath(dir, uid)
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
	if err := WriteEventsFile(tmp, events); err != nil {
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
	// The plain file is older than the one just written, and readers
	// prefer the compressed one, so removing it changes nothing they
	// see.
	if err := os.Remove(filepath.Join(dir, uid+LegacyEventsExt)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("normalize: %w", err)
	}
	return nil
}
