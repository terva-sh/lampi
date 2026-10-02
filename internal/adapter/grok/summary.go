package grok

import (
	"bytes"
	"encoding/json"
	"os"
)

// fileIdent is what the memo keeps for one summary.json. updates.jsonl
// stores an empty ident: the native id is the directory UUID, and the
// cwd, title, and model live on the companion.
type fileIdent struct {
	CWD   string `json:"cwd,omitempty"`
	Title string `json:"title,omitempty"`
	Model string `json:"model,omitempty"`
}

// summaryFile is the subset of Grok Build summary.json this reader
// uses. Other keys stay in the uploaded bytes.
type summaryFile struct {
	Info struct {
		CWD string `json:"cwd"`
	} `json:"info"`
	SessionSummary string          `json:"session_summary"`
	GeneratedTitle string          `json:"generated_title"`
	Model          json.RawMessage `json:"current_model_id"`
}

// readSummary reads cwd, title, and model from summary.json. A file
// that is not JSON yet is an empty ident and a nil error: the bytes
// still upload, and the cwd falls back to the group directory. An
// unreadable file is an error and the companion is left out.
func readSummary(path string) (fileIdent, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fileIdent{}, err
	}
	var doc summaryFile
	if json.Unmarshal(raw, &doc) != nil {
		return fileIdent{}, nil
	}
	title := doc.GeneratedTitle
	if title == "" {
		title = doc.SessionSummary
	}
	return fileIdent{CWD: doc.Info.CWD, Title: title, Model: jsonString(doc.Model)}, nil
}

func jsonString(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}
