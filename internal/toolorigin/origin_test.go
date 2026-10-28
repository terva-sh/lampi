package toolorigin

import "testing"

// The names are the shapes each harness wrote in the calibration corpus
// (docs/session-diagnostics-calibration.md). Built-in names are real;
// server, plugin and namespace names stand in for private ones.
func TestOf(t *testing.T) {
	builtin := Origin{Kind: Builtin}
	unknown := Origin{Kind: Unknown}
	mcp := func(server string) Origin { return Origin{Kind: MCP, Name: server} }
	for _, c := range []struct {
		harness, name string
		want          Origin
	}{
		// Claude Code.
		{"claude", "Bash", builtin},
		{"claude", "Read", builtin},
		{"claude", "WebFetch", builtin},
		{"claude", "AskUserQuestion", builtin},
		{"claude", "mcp__issue-tracker__list_issues", mcp("issue-tracker")},
		{"claude", "mcp__docs__search", mcp("docs")},
		{"claude", "mcp__plugin_toolkit_files__read", Origin{Kind: Plugin, Name: "toolkit", Server: "files"}},
		{"claude", "mcp__plugin_my-plugin_my_server__run", Origin{Kind: Plugin, Name: "my-plugin", Server: "my_server"}},
		{"claude", "mcp__plugin_toolkit__read", unknown},
		// Names the harness does not offer: a missing or extra prefix.
		{"claude", "mcp_docs_search", unknown},
		{"claude", "docs__search", unknown},
		{"claude", "mcp__docs", unknown},
		{"claude", "mcp____search", unknown},
		{"claude", "bash", unknown},
		{"claude", "functions.Bash", unknown},
		// Codex.
		{"codex", "exec", builtin},
		{"codex", "shell", builtin},
		{"codex", "apply_patch", builtin},
		{"codex", "exec_command", builtin},
		{"codex", "send_message", builtin},
		{"codex", "mcp__issue_tracker__list_issues", mcp("issue_tracker")},
		{"codex", "clock.sleep", Origin{Kind: Extension, Name: "clock"}},
		{"codex", "collab:wait", Origin{Kind: Extension, Name: "collab"}},
		{"codex", "clock.", unknown},
		{"codex", "Exec", unknown},
		// Cursor CLI.
		{"cursor-cli", "Shell", builtin},
		{"cursor-cli", "StrReplace", builtin},
		{"cursor-cli", "mcp__cursor__AwaitShell", builtin},
		{"cursor-cli", "mcp__issue-tracker__list_issues", mcp("issue-tracker")},
		{"cursor-cli", "CallDynamicTool", unknown},
		{"cursor-cli", "shell_command", unknown},
		// OpenCode and harnesses with no recorded naming.
		{"opencode", "read", builtin},
		{"grok", "run_terminal_cmd", builtin},
		{"terva", "mcp__docs__search", mcp("docs")},
		// A plugin prefix is Claude Code's only.
		{"codex", "mcp__plugin_toolkit_files__read", mcp("plugin_toolkit_files")},
		// No name.
		{"claude", "", unknown},
		{"codex", "", unknown},
		{"opencode", "two words", unknown},
	} {
		if got := Of(c.harness, c.name); got != c.want {
			t.Errorf("%s %q: %+v, want %+v", c.harness, c.name, got, c.want)
		}
	}
}
