package recall

import "strings"

// Origin kinds: where a tool comes from (TKT-01M61MZN3).
const (
	// OriginBuiltin is a tool the harness itself offers.
	OriginBuiltin = "builtin"
	// OriginMCP is a tool of an MCP server the user configured. Name is
	// the server.
	OriginMCP = "mcp"
	// OriginPlugin is a tool of an MCP server that a Claude Code plugin
	// brings. Name is the plugin and Server its server.
	OriginPlugin = "plugin"
	// OriginExtension is a tool of a harness's own tool group, such as
	// a Codex namespace. Name is the group.
	OriginExtension = "extension"
	// OriginUnknown is a name the rules do not place: empty, or a shape
	// the harness does not give its tools. In Claude Code that is a
	// name the model wrote wrong, and every such call fails
	// (docs/session-diagnostics-calibration.md).
	OriginUnknown = "unknown"
)

// OriginKinds lists the kinds, for a filter that validates its value.
var OriginKinds = []string{OriginBuiltin, OriginMCP, OriginPlugin, OriginExtension, OriginUnknown}

// Origin says where a tool comes from. It is derived from the harness
// and the tool name when a query runs, not stored, so a rule can change
// without rebuilding the index.
type Origin struct {
	Kind   string `json:"kind"`
	Name   string `json:"name,omitempty"`
	Server string `json:"server,omitempty"`
}

// mcpPrefix opens an MCP tool name in every harness that names one:
// mcp__<server>__<tool>. Claude Code writes it that way, and the Codex
// and Cursor CLI normalizers write their MCP calls the same way.
const mcpPrefix = "mcp__"

// cursorNamespace is the namespace Cursor CLI gives its own tools when
// it calls them through CallDynamicTool (AwaitShell, TodoWrite, Task),
// which its normalizer writes as mcp__cursor__<tool>.
const cursorNamespace = "cursor"

// ToolOrigin places toolName, called in harness. The rules come from
// the calibration spike's naming table:
//
//   - Every harness: mcp__<server>__<tool> is an MCP server's tool.
//   - Claude Code: mcp__plugin_<plugin>_<server>__<tool> is a plugin's
//     server. A bare name of letters and digits that starts with a
//     capital (Bash, Read, WebFetch) is built in.
//   - Codex: a bare snake_case name (exec, shell, apply_patch,
//     exec_command) is built in. A name with a namespace,
//     <namespace>.<tool> or <namespace>:<tool> (clock.sleep,
//     collab:wait), is an extension. An MCP call outside code mode
//     that a normalizer named by its bare tool, dropping the server,
//     reads as built in until the projection writes mcp__ names.
//   - Cursor CLI: a bare name of letters (Shell, Read, StrReplace) is
//     built in, and mcp__cursor__<tool> is Cursor's own tool reached
//     through CallDynamicTool, so built in too. CallDynamicTool itself
//     is unknown: left unprojected, it hides which tool it called.
//   - Any other harness: a bare identifier is built in.
//
// Everything else is unknown, with no name.
func ToolOrigin(harness, toolName string) Origin {
	if toolName == "" {
		return Origin{Kind: OriginUnknown}
	}
	if rest, ok := strings.CutPrefix(toolName, mcpPrefix); ok {
		server, tool, ok := strings.Cut(rest, "__")
		if !ok || server == "" || tool == "" {
			return Origin{Kind: OriginUnknown}
		}
		if harness == "claude" {
			if p, ok := strings.CutPrefix(server, "plugin_"); ok {
				plugin, srv, ok := strings.Cut(p, "_")
				if ok && plugin != "" && srv != "" {
					return Origin{Kind: OriginPlugin, Name: plugin, Server: srv}
				}
				return Origin{Kind: OriginUnknown}
			}
		}
		if harness == "cursor-cli" && server == cursorNamespace {
			return Origin{Kind: OriginBuiltin}
		}
		return Origin{Kind: OriginMCP, Name: server}
	}
	switch harness {
	case "claude":
		if capitalWord(toolName) {
			return Origin{Kind: OriginBuiltin}
		}
	case "codex":
		if i := strings.IndexAny(toolName, ".:"); i > 0 && i < len(toolName)-1 {
			ns, tool := toolName[:i], toolName[i+1:]
			if identifier(ns) && identifier(tool) {
				return Origin{Kind: OriginExtension, Name: ns}
			}
			return Origin{Kind: OriginUnknown}
		}
		if snakeCase(toolName) {
			return Origin{Kind: OriginBuiltin}
		}
	case "cursor-cli":
		// CallDynamicTool left unprojected hides which tool it called.
		if capitalWord(toolName) && toolName != "CallDynamicTool" {
			return Origin{Kind: OriginBuiltin}
		}
	default:
		if identifier(toolName) {
			return Origin{Kind: OriginBuiltin}
		}
	}
	return Origin{Kind: OriginUnknown}
}

// capitalWord is an ASCII capital followed by letters and digits.
func capitalWord(s string) bool {
	if s == "" || s[0] < 'A' || s[0] > 'Z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isAlnum(s[i]) {
			return false
		}
	}
	return true
}

// snakeCase is a lowercase ASCII letter followed by lowercase letters,
// digits and underscores.
func snakeCase(s string) bool {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

// identifier is an ASCII letter followed by letters, digits, hyphens and
// underscores.
func identifier(s string) bool {
	if s == "" || !(s[0] >= 'a' && s[0] <= 'z' || s[0] >= 'A' && s[0] <= 'Z') {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isAlnum(s[i]) && s[i] != '_' && s[i] != '-' {
			return false
		}
	}
	return true
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
