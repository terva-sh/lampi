---
schema: 3
id: TKT-01M61MYZ6H6J8MGNPWHS8MTJ1X
title: Session diagnostics, noisy tools and tool usage statistics
type: epic
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/search
  - area/normalize
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T16:19:47Z
updated_at: 2026-10-28T16:29:50Z
created_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
extensions: {}
---

## Description

Give operators and agents a way to diagnose sessions that went badly, to find tools, MCP servers and extensions that flood a session's context, and to see which tools are used, when and how, so that harness and client configuration can be tuned from evidence.

This came from an exploration on 2026-10-28 (Claude Code session t3-049834bc). The normalized events already carry most of the signal: `event_type` (`tool_call`, `tool_result`, `usage`, `compaction`, `error`), `tool.name`, `tool.call_id`, `tool.is_error`, token usage and `content_text` (internal/normalize/event.go). Nothing assembles it yet.

### Three parts

1. **Tool usage statistics.** Which tools are called, how often, when, by which harness, project and model, with error rates, input and output sizes, and call-to-result latency, bucketed over time.
2. **Large tool blocks and noisy servers.** Attribute tool output size to its origin (built-in tool, MCP server, plugin or extension), so a server whose results dominate sessions stands out. Claude Code names MCP tools `mcp__<server>__<tool>`; other harnesses need checking.
3. **Session diagnosis.** A streaming pass over one session that reports findings with stable codes: large tool results or inputs, error streaks, retry loops (same tool and input hash), repeated reads of one file, context pressure (input token growth, compactions, falling cache reads), orphaned calls or results, and unreadable lines. Each code maps to a fixed suggestion, shipped embedded in the binary like internal/advisory/agents.json.

### Gaps found in the current data

- Claude Code tool results carry no tool name: `toolResultEvent` (internal/normalize/claude.go:304) sets only `CallID`. The result is the large block, so results cannot be grouped by tool without joining on `call_id`.
- The search index truncates content at `IndexContentMax` = 256 KiB (internal/recall/index.go:61, :706) and stores no original size, so `length(content)` hides exactly the largest blocks. It also stores no `call_id` or model id.
- `tool.is_error` is set by the Claude, Codex, Grok and terva normalizers only. OpenCode, Cursor IDE, Cursor CLI and grokbot leave it null, so error rates are blank for those harnesses.

### Constraints

- Report names, counts, sizes, times and event positions, never content. This follows the counting rule in internal/recall/count.go: only numbers cross the network. Results stay inside a read token's scope.
- Bounded memory. Any per-session pass streams the events file once (see docs/performance.md and the v0.9.0 memory work, TKT-01M56GHA).
- Prefer aggregates over the existing search index (docs table, indexed on tool_name) to new per-session state. Add columns once, together, because each signature change rewrites every row.

### Alternatives considered

- Computing statistics from the raw events files per request: simple, but a lake-wide view would re-read every session. Rejected in favour of index columns.
- A separate precomputed per-session summary store: more state to keep current as transcripts grow. Deferred until the index aggregates prove too slow.
- Generated (LLM) suggestions: rejected; suggestions are static text keyed by finding code, so they are reviewable and versioned.
- Estimating tokens per tool result: no harness reports them; bytes are the ranking measure, with an optional estimate from the next `usage` delta later.

## Acceptance criteria

- [ ] Operators can see which tools and origins are used most, when, and at what size, error and latency cost.
- [ ] Operators can find the sessions and MCP servers or extensions with the largest tool blocks.
- [ ] A session can be diagnosed into coded findings with fixed suggestions, from the CLI, API, MCP and dashboard.
- [ ] No diagnostic or statistic returns transcript content, and all respect read-token scope.
