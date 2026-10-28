package diagnose

import (
	"bufio"
	"encoding/json"
	"strings"

	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/toolorigin"
)

// InnerRawType is the raw_type of a Codex code-mode inner call or
// result (docs/harnesses.md, "Codex code-mode inner calls"): a tool the script in an exec cell called, recorded by Codex
// as an item_completed event. Its result went to the script, not to the
// model, so it is not context: size rules read the cell's result
// instead, and retry and orphan rules read only the calls the model
// made. Inner results still count for noisy_tool_origin, in their own
// pool, because they show which server produced the bulk.
const InnerRawType = "item_completed"

func inner(harness, rawType string) bool {
	return harness == protocol.HarnessCodex && rawType == InnerRawType
}

// fileArgs names the input fields that hold a file path for one tool of
// one harness. Range fields, when set, are what makes two reads of one
// path the same read. These are the structured fields the calibration
// spike found ("Where each harness keeps a read path"); shell reads are
// not parsed.
type fileArgs struct {
	path   []string
	ranges []string
}

var (
	claudeRange = []string{"offset", "limit"}
	readTools   = map[string]map[string]fileArgs{
		protocol.HarnessClaude:    {"Read": {[]string{"file_path"}, claudeRange}},
		protocol.HarnessCursorCLI: {"Read": {[]string{"path"}, claudeRange}},
		protocol.HarnessOpenCode:  {"read": {[]string{"filePath", "path"}, claudeRange}},
	}
	writeTools = map[string]map[string]fileArgs{
		protocol.HarnessClaude: {
			"Edit": {path: []string{"file_path"}}, "Write": {path: []string{"file_path"}},
			"MultiEdit": {path: []string{"file_path"}}, "NotebookEdit": {path: []string{"notebook_path"}},
		},
		protocol.HarnessCursorCLI: {"Write": {path: []string{"path"}}, "StrReplace": {path: []string{"path"}}},
		protocol.HarnessOpenCode:  {"write": {path: []string{"filePath", "path"}}, "edit": {path: []string{"filePath", "path"}}},
	}
)

// readArgs is the path and range of a structured read, or ok false when
// the call is not one.
func readArgs(harness, name, input string) (path, rng string, ok bool) {
	spec, ok := readTools[harness][name]
	if !ok {
		return "", "", false
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(input), &obj) != nil {
		return "", "", false
	}
	path = firstString(obj, spec.path)
	if path == "" {
		return "", "", false
	}
	var b strings.Builder
	for _, k := range spec.ranges {
		b.WriteString(k)
		b.WriteByte('=')
		b.Write(obj[k])
		b.WriteByte(0)
	}
	return path, b.String(), true
}

// writePaths calls fn with each path a call writes: the structured
// path field of a write tool, or each file header of a Codex
// apply_patch. Codex code-mode FileChange items are not read yet.
func writePaths(harness, name, input string, fn func(path string)) {
	if harness == protocol.HarnessCodex && name == "apply_patch" {
		patchPaths(input, fn)
		return
	}
	spec, ok := writeTools[harness][name]
	if !ok {
		return
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(input), &obj) != nil {
		return
	}
	if p := firstString(obj, spec.path); p != "" {
		fn(p)
	}
}

var patchHeaders = []string{"*** Update File: ", "*** Add File: ", "*** Delete File: ", "*** Move to: "}

func patchPaths(input string, fn func(string)) {
	// The input is the patch, or an object holding it under input.
	var obj struct{ Input string }
	if strings.HasPrefix(strings.TrimSpace(input), "{") && json.Unmarshal([]byte(input), &obj) == nil && obj.Input != "" {
		input = obj.Input
	}
	sc := bufio.NewScanner(strings.NewReader(input))
	sc.Buffer(nil, 64<<10)
	for sc.Scan() {
		line := sc.Text()
		for _, h := range patchHeaders {
			if p, ok := strings.CutPrefix(line, h); ok && strings.TrimSpace(p) != "" {
				fn(strings.TrimSpace(p))
			}
		}
	}
}

func firstString(obj map[string]json.RawMessage, keys []string) string {
	for _, k := range keys {
		var s string
		if json.Unmarshal(obj[k], &s) == nil && s != "" {
			return s
		}
	}
	return ""
}

// origin is the MCP server a call went to, or "" for any other tool.
// A plugin's server reads as <plugin>/<server>, so it is not taken for
// a server of the same name that the user configured. The rules are
// toolorigin's, the same the tool usage statistics group by.
func origin(harness, name string) string {
	switch o := toolorigin.Of(harness, name); o.Kind {
	case toolorigin.MCP:
		return o.Name
	case toolorigin.Plugin:
		return o.Name + "/" + o.Server
	}
	return ""
}

// unknownToolMarker is the text Claude Code returns, in an error
// result, for a call to a tool it does not offer. The offered tool list
// is not in the normalized events, so this is the only harness the rule
// can read; the result text is matched here and never returned.
const unknownToolMarker = "No such tool available"
