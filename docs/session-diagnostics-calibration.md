# Session diagnostics calibration

This page records the measurements behind the default thresholds for
session diagnosis findings, how often each candidate signal fires, how
each harness names MCP and extension tools, where each harness keeps the
path of a file read, and whether call and result timestamps support
latency. It comes from the calibration spike TKT-01M61MZN03 under the
epic TKT-01M61MYZ6H (session diagnostics, noisy tools and tool usage
statistics).

The page reports counts, sizes, rates and times only. It names built-in
tools, because those names are the same for every user of a harness. It
does not name MCP servers, projects, paths or sessions.

## Method

### Corpus

The spike read one workstation's local transcript stores, the same files
the agent uploads to a lake. It did not read a lake.

| Harness | Store | Sessions | Sessions with tool calls | Tool calls |
|---|---|---:|---:|---:|
| Claude Code, main sessions | projects JSONL | 110 | 102 | 68,302 |
| Claude Code, subagent sessions | projects `subagents/` JSONL | 894 | 894 | 98,499 |
| Codex | sessions JSONL | 88 | 84 | 33,158 calls, 44,526 inner calls |
| Cursor CLI | `store.db` (chats and ACP sessions) | 12 | 8 | 26,525 |
| OpenCode | database | 8 | 3 | 3 |

No Cursor IDE, Grok, grokbot or terva store existed on the machine, so
those harnesses have no numbers here.

The corpus is one person's work. It has one MCP server in real use, and
Codex runs in code mode (described below). Treat the rates as a first
calibration, and repeat the measurement on a lake with more users before
tightening a threshold.

### How the numbers were produced

- Claude Code and Codex files were streamed through lampi's own
  normalizers (`normalize.Claude` and `normalize.Codex`, `Stream`), so
  their numbers describe normalized events. The program kept per-session
  state only: one record per call id, and counters.
- A Claude Code `tool_result` carries no `tool.name`. Results were joined
  to calls on `tool.call_id` within the session.
- Codex writes both a `response_item` (`function_call`,
  `custom_tool_call`) and, for some tools, an `event_msg`
  (`*_begin`/`*_end`) with the same call id. The two were merged per call
  id, so a call counts once.
- Codex in code mode also records the tool calls made inside each `exec`
  cell as `event_msg` lines of type `item_completed`. The Codex
  normalizer projects those lines as `unknown` events. They were read
  from the raw lines and are reported separately as **Codex inner
  calls**.
- The Cursor CLI normalizer projects a blob whose `content` is a parts
  array as one `unknown` event, so it emits no `tool_call` or
  `tool_result`. Cursor CLI numbers come from the AI SDK parts in the
  blobs (`tool-call` and `tool-result`), read from a copy of each
  `store.db`, in `rowid` order. They are raw, not normalized.
- OpenCode was inspected structurally only, from a copy of the database.
- Every SQLite store was copied to a scratch directory and only the copy
  was opened. No source file was written.
- Sizes are bytes of UTF-8 `content_text`: the call's arguments for a
  call, and the result text for a result. Image bytes are not included.
- Percentiles are nearest-rank.

The program was a throwaway Go `main` package placed inside the module,
so it could import `internal/normalize`, and was deleted afterwards. To
reproduce the numbers on a lake, apply the definitions below to the
normalized `.jsonl.zst` events files (DuckDB reads them directly; see
the comment at the top of `internal/normalize/eventsfile.go`), grouping
by `session_id` and ordering by event position. For Codex inner calls
and Cursor CLI tool parts, read the `extra` of the `unknown` events until
the normalizers project them.

### Definitions used for the smells

- **Polling tools** are excluded from retry detection, because repeating
  them with the same input is how they work: Codex `sleep`, `wait`,
  `wait_agent`, `list_agents`, `clock.sleep` and `collab:wait`; Claude
  Code `Monitor`, `TaskStop` and `ListAgents`; Cursor CLI `AwaitShell`
  and `GetDynamicTools`.
- A **retry** is a call whose tool name and input SHA-256 equal one of the
  previous K non-polling calls in the same session. A retry with the
  **same output** also has the same result SHA-256 as that earlier call.
- An **error streak** is a run of consecutive results with
  `is_error = true`, in call order. In Claude Code, a missing `is_error`
  means not an error: Claude Code writes the flag on every `Bash` result
  but on other tools only when it is true.
- A **repeated read** counts reads of one path, reset when a write to
  that path is seen. An **identical read** also has the same input, so
  the same offset and limit.
- **Context tokens** for a usage event are `input + cache_read +
  cache_write` for Claude Code, and `input` for Codex, where cached input
  is part of `input`. Consecutive identical usage events were counted
  once, because Claude Code repeats the usage of one message on each of
  its content-block lines.
- A **cache-read collapse** is a usage event whose `cache_read` falls
  below half of the previous event's (when that was over 20,000 tokens),
  while context tokens stay at 80% or more of the previous value. A
  collapse after an idle gap of 5 minutes or more is counted separately,
  because prompt caches expire.

## Tool call and result sizes

Input and result sizes are p50 / p90 / p95 / p99 / max.

### By origin

| Harness | Origin | Calls | Input | Result | Error rate |
|---|---|---:|---|---|---:|
| Claude Code | built-in | 164,014 | 325 B / 1.9 KiB / 3.2 KiB / 8.1 KiB / 121 KiB | 603 B / 5.3 KiB / 8.3 KiB / 18.4 KiB / 77.2 KiB | 3.1% |
| Claude Code | MCP | 2,689 | 62 B / 89 B / 173 B / 2.8 KiB / 11.8 KiB | 165 B / 593 B / 3.6 KiB / 14.1 KiB / 44.6 KiB | 3% |
| Claude Code | other | 128 | 168 B / 3.2 KiB / 8.3 KiB / 20.5 KiB / 20.6 KiB | 72 B / 75 B / 78 B / 96 B / 96 B | 100% |
| Codex | built-in | 33,158 | 606 B / 3.1 KiB / 5.0 KiB / 10.6 KiB / 37.8 KiB | 1.0 KiB / 13.4 KiB / 22.7 KiB / 39.2 KiB / 50.9 KiB | not recorded |
| Codex inner calls | built-in | 40,038 | 269 B / 2.3 KiB / 4.1 KiB / 9.7 KiB / 55.6 KiB | 590 B / 11.7 KiB / 18.9 KiB / 49.8 KiB / 1.6 MiB | 9.5% |
| Codex inner calls | MCP | 4,488 | 65 B / 286 B / 513 B / 13.9 KiB / 130 KiB | 526 B / 12.1 KiB / 35.9 KiB / 160 KiB / 1.0 MiB | 5.7% |
| Cursor CLI | built-in | 25,740 | 185 B / 869 B / 1.3 KiB / 3.7 KiB / 48.2 KiB | 417 B / 2.9 KiB / 4.5 KiB / 11.5 KiB / 71.3 KiB | not recorded |
| Cursor CLI | MCP | 785 | 203 B / 240 B / 244 B / 254 B / 277 B | 210 B / 12.6 KiB / 19.2 KiB / 35.2 KiB / 38.5 KiB | not recorded |

The Claude Code **other** origin is tool names the harness does not
offer: names the model wrote with a missing or extra prefix. Every one of
those 128 calls failed. That makes an unknown tool name a cheap and
reliable finding.

Codex error rates come from `exit_code != 0` on inner commands, which
also counts a search that found nothing. The normalized Codex `exec`
result records no error flag. Cursor CLI parts carry no error flag.

### By built-in tool

Tools with at least 50 calls.

| Harness | Tool | Calls | Input | Result |
|---|---|---:|---|---|
| Claude Code | `Bash` | 140,946 | 313 B / 1.4 KiB / 2.5 KiB / 5.4 KiB / 54.4 KiB | 723 B / 5.2 KiB / 7.9 KiB / 16.1 KiB / 29.3 KiB |
| Claude Code | `Read` | 6,012 | 122 B / 175 B / 184 B / 193 B / 217 B | 3.5 KiB / 21.1 KiB / 31.9 KiB / 47.9 KiB / 77.2 KiB |
| Claude Code | `Edit` | 6,581 | 716 B / 2.1 KiB / 3.0 KiB / 5.3 KiB / 29.1 KiB | 173 B / 241 B / 256 B / 383 B / 1.7 KiB |
| Claude Code | `Write` | 6,270 | 3.0 KiB / 11.7 KiB / 15.7 KiB / 27.3 KiB / 121 KiB | 171 B / 233 B / 245 B / 260 B / 285 B |
| Claude Code | `Agent` | 899 | 2.8 KiB / 5.3 KiB / 6.1 KiB / 8.0 KiB / 13.6 KiB | 1.1 KiB / 1.1 KiB / 1.1 KiB / 10.1 KiB / 19.1 KiB |
| Claude Code | `WebFetch` | 226 | 327 B / 592 B / 715 B / 1017 B / 1.2 KiB | 1.9 KiB / 5.5 KiB / 19.3 KiB / 35.3 KiB / 45.6 KiB |
| Claude Code | `WebSearch` | 110 | 81 B / 114 B / 123 B / 146 B / 146 B | 3.8 KiB / 4.8 KiB / 5.7 KiB / 6.7 KiB / 8.5 KiB |
| Claude Code | `AskUserQuestion` | 409 | 1.2 KiB / 2.4 KiB / 2.8 KiB / 3.5 KiB / 3.8 KiB | 357 B / 795 B / 895 B / 1.3 KiB / 17.6 KiB |
| Claude Code | `SendMessage` | 679 | 1.1 KiB / 2.4 KiB / 2.9 KiB / 4.4 KiB / 5.5 KiB | 163 B / 326 B / 349 B / 370 B / 385 B |
| Claude Code | `Monitor` | 907 | 355 B / 668 B / 812 B / 1.1 KiB / 2.2 KiB | 308 B / 308 B / 308 B / 485 B / 556 B |
| Claude Code | `TaskStop` | 825 | 23 B / 23 B / 23 B / 23 B / 31 B | 653 B / 1.3 KiB / 2.0 KiB / 5.3 KiB / 13.8 KiB |
| Codex | `exec` (code-mode cell) | 28,371 | 619 B / 3.5 KiB / 5.5 KiB / 11.2 KiB / 37.8 KiB | 1.5 KiB / 15.4 KiB / 25.1 KiB / 39.2 KiB / 50.9 KiB |
| Codex | `send_message` | 2,492 | 805 B / 1.3 KiB / 1.5 KiB / 2.1 KiB / 2.8 KiB | 0 B / 0 B / 0 B / 0 B / 46 B |
| Codex | `sleep` | 1,458 | 21 B | 43 B / 43 B / 43 B / 58 B / 58 B |
| Codex inner | `exec_command` | 35,748 | 263 B / 1.9 KiB / 3.2 KiB / 7.8 KiB / 34.2 KiB | 817 B / 12.8 KiB / 20.4 KiB / 54.3 KiB / 1.6 MiB |
| Codex inner | `apply_patch` | 2,449 | 2.3 KiB / 8.5 KiB / 10.8 KiB / 19.2 KiB / 55.6 KiB | 145 B / 290 B / 368 B / 567 B / 834 B |
| Cursor CLI | `Shell` | 6,918 | 410 B / 1.2 KiB / 1.7 KiB / 3.4 KiB / 10.3 KiB | 635 B / 2.9 KiB / 3.8 KiB / 8.1 KiB / 19.9 KiB |
| Cursor CLI | `Read` | 6,886 | 133 B / 157 B / 165 B / 176 B / 190 B | 1.2 KiB / 4.5 KiB / 6.8 KiB / 15.4 KiB / 71.3 KiB |
| Cursor CLI | `Grep` | 3,850 | 156 B / 206 B / 226 B / 288 B / 419 B | 596 B / 3.6 KiB / 6.4 KiB / 14.7 KiB / 14.8 KiB |
| Cursor CLI | `StrReplace` | 3,710 | 340 B / 1.0 KiB / 1.5 KiB / 3.0 KiB / 9.6 KiB | 130 B / 144 B / 149 B / 163 B / 790 B |
| Cursor CLI | `Write` | 2,589 | 468 B / 2.8 KiB / 4.2 KiB / 8.0 KiB / 48.2 KiB | 123 B / 143 B / 147 B / 156 B / 168 B |
| Cursor CLI | `Glob` | 579 | 127 B / 148 B / 155 B / 173 B / 206 B | 202 B / 1.4 KiB / 2.4 KiB / 4.9 KiB / 4.9 KiB |
| Cursor CLI | `AwaitShell` | 818 | 131 B / 146 B / 162 B / 204 B / 245 B | 221 B / 236 B / 239 B / 243 B / 247 B |

### What the sizes show

- **Harnesses cap results before the model sees them.** Claude Code
  `Bash` results stop at about 30 KiB, because Claude Code saves larger
  output to a file and returns a pointer. The largest Claude Code result
  in the corpus is 77 KiB, from `Read`. Codex code-mode cell output stops
  near 39 KiB: 614 cells returned between 39 and 40 KiB, and that was the
  largest cell result in 64 of the 84 Codex sessions with tool calls. A
  threshold above a harness's cap never fires for that harness.
- **Codex inner results are not context.** In code mode the model sees
  only what the `exec` cell returns. An inner command or MCP result of
  1 MiB is data the cell's script handled, not tokens the model read. The
  sizes that cost context are the `exec` results; the inner results show
  which tool produced the bulk.
- **Inputs are large only for writes.** `Write`, `apply_patch` and
  `Edit` hold the large inputs. A large input is a file the model
  generated, which costs output tokens and then context.

## Share of a session's tool result bytes

Among sessions with at least 64 KiB of tool result bytes.

| Group | Sessions | Largest tool's share, p50 / p90 | Largest tool over 80% | With MCP result bytes | MCP server's share, p50 / p90 (of those) | Server over 25% | Server over 25% and 256 KiB or more |
|---|---:|---|---:|---:|---|---:|---:|
| Claude Code, main | 91 | 0.93 / 0.99 | 86.8% | 81 | 0.01 / 0.05 | 1.1% | 0% |
| Claude Code, subagent | 842 | 0.92 / 1.00 | 71.3% | 110 | 0.00 / 0.00 | 0% | 0% |
| Codex (cells) | 78 | 1.00 / 1.00 | 100% | 0 | - | 0% | 0% |
| Codex inner calls | 78 | 0.99 / 1.00 | 83.3% | 39 | 0.09 / 0.51 | 12.8% | 10.7% of sessions with calls |
| Cursor CLI | 8 | 0.54 / 0.71 | 0% | 5 | 0.04 / 0.09 | 0% | 0% |

The largest tool is the shell in almost every session (`Bash`, `exec`,
`exec_command`, or Cursor `Read`/`Shell`), so "one tool dominates" is
the normal shape and is not a finding. The share held by one MCP server
or extension is the signal that separates sessions: it is near zero in
most, and above a quarter in about one Codex session in eight.

## Smell firing rates

Each cell is the share of sessions with at least one tool call where the
smell fires, except the context rows, which count all sessions. Codex
context and compaction rows come from normalized events; Codex inner
calls carry no usage.

| Smell and threshold | Claude main | Claude subagent | Codex (cells) | Codex inner | Cursor CLI |
|---|---:|---:|---:|---:|---:|
| Any result over 16 KiB | 68.6% | 75.7% | 92.9% | 92.9% | 6 of 8 |
| Any result over 32 KiB | 10.8% | 20.5% | 84.5% | 84.5% | 4 of 8 |
| Any result over 64 KiB | 0% | 0.2% | 0% | 41.7% | 4 of 8 |
| Any result over 256 KiB | 0% | 0% | 0% | 27.4% | 0 of 8 |
| 3 or more results over 32 KiB | 1.0% | 2.7% | 65.5% | 58.3% | 4 of 8 |
| 10 or more results over 32 KiB | 0% | 0% | 29.8% | 31.0% | 2 of 8 |
| Any input over 16 KiB | 28.4% | 20.5% | 40.5% | 44.0% | 4 of 8 |
| Any input over 32 KiB | 6.9% | 3.5% | 3.6% | 8.3% | 3 of 8 |
| Any input over 64 KiB | 0% | 0.3% | 0% | 2.4% | 0 of 8 |
| Error streak of 3 | 6.9% | 5.1% | n/a | 31.0% | n/a |
| Error streak of 5 | 0% | 0.7% | n/a | 4.8% | n/a |
| Error streak of 8 | 0% | 0.1% | n/a | 1.2% | n/a |
| 3 or more retries, K = 5 | 7.8% | 2.6% | 27.4% | 47.6% | 3 of 8 |
| 5 or more retries, K = 10 | 4.9% | 1.3% | 23.8% | 42.9% | 3 of 8 |
| 3 identical calls in a row | 0% | 0.7% | 17.9% | 20.2% | 2 of 8 |
| 3 or more same-output retries, K = 5 | 2.9% | 0.2% | 0% | 33.3% | 2 of 8 |
| 5 or more same-output retries, K = 5 | 1.0% | 0.1% | 0% | 28.6% | 1 of 8 |
| 1 or more retries of a failed call, K = 5 | 3.9% | 2.1% | n/a | 33.3% | n/a |
| 3 or more retries of a failed call, K = 5 | 1.0% | 0% | n/a | 17.9% | n/a |
| One path read 5 times, no write between | 3.9% | 3.1% | n/a | 25.0% | 4 of 8 |
| One path read 10 times, no write between | 0% | 0.2% | n/a | 8.3% | 4 of 8 |
| Identical read 3 times, no write between | 4.9% | 0.2% | n/a | 14.3% | 4 of 8 |
| Identical read 5 times, no write between | 1.0% | 0% | n/a | 6.0% | 4 of 8 |
| 1 or more compactions | 51.8% | 4.5% | 53.4% | 40.9% | 0 of 12 |
| 3 or more compactions | 23.6% | 0.2% | 29.5% | 28.4% | 0 of 12 |
| Context peak of 200k tokens or more | 64.5% | 50.2% | 39.8% | n/a | n/a |
| Context peak 10 times the first | 0.9% | 20.2% | 46.6% | n/a | n/a |
| Cache-read collapse without idle gap, 1 or more | 7.3% | 3.5% | 44.3% | n/a | n/a |
| Cache-read collapse without idle gap, 3 or more | 0% | 0.1% | 30.7% | n/a | n/a |
| Cache-read collapse after 5 or more minutes idle | 47.3% | 38.5% | 36.4% | n/a | n/a |
| 1 or more calls without a result | 11.8% | 0.1% | 0% | 0% | 7 of 8 |
| 3 or more calls without a result | 1.0% | 0% | 0% | 0% | 5 of 8 |
| 1 or more results without a call | 11.8% | 0% | 0% | 0% | 7 of 8 |
| 1 or more unreadable lines | 0.9% | 0% | 0% | 0% | n/a |

Notes on the rates:

- Codex inner retries are loops inside one code-mode cell, such as a
  script that polls a command until its output changes. They do not
  enter the model's context one by one, and at the cell level no Codex
  session repeated a call with the same output. Run retry detection on
  the calls the model made: Codex cells, not inner calls.
- Codex exit-code errors fire streaks often, because a search with no
  match exits 1. Claude Code's explicit `is_error` fires a streak of 5 in
  under 1% of sessions.
- Cursor CLI path and ordering rates are not reliable. Blobs are content
  addressed and were read in `rowid` order, which is not always the
  conversation order; every orphan in Cursor CLI is likely an ordering
  artefact. Cursor `Read` is also paged with `offset`/`limit`, so a
  path-only count measures paging.
- A Claude Code result without a call, or a call without a result, is
  often a session that was resumed in another file or stopped mid-call.
  Neither was told apart here.
- Context-token peaks reach 470k in Claude Code and 259k in Codex, so the
  models in use have different windows. An absolute token threshold
  measures the model, not the session.
- Of 1,454 Claude Code cache-read collapses, 1,412 follow an idle gap of
  5 minutes or more, the prompt cache's lifetime. Codex's cached-input
  count falls often without a gap, so it is not a usable cache signal.

## Recommended defaults

Severities are `info` (worth knowing), `warn` (likely cost or waste) and
`error` (data or session damage). "Fires on" quotes the rates above for
Claude Code main sessions, Claude Code subagents and Codex cells.

| Code | Default rule | Severity | Fires on | Why |
|---|---|---|---|---|
| `large_tool_result` | One result of 64 KiB or more; or 10 or more results of 32 KiB or more | warn | 0% / 0.2% / 0%; with the count rule 0% / 0% / 29.8% | 64 KiB sits above the Claude Code and Codex caps, so it fires only where no cap applied. The count rule catches a session that keeps filling the cap. 32 KiB alone fires on most Codex sessions and on 21% of subagents. |
| `large_tool_input` | One input of 32 KiB or more | info | 6.9% / 3.5% / 3.6% | Large inputs are generated files. 16 KiB fires on a quarter of sessions; 64 KiB almost never. Keep it informational. |
| `error_streak` | 5 or more consecutive results with `is_error = true` | warn | 0% / 0.7% / n/a (inner 4.8%) | 3 fires on 5 to 7% of Claude Code sessions and 31% of Codex inner runs, where an exit code of 1 is often not a failure. 5 separates a stuck session. |
| `retry_loop` | 3 or more calls, each equal in tool, input hash and result hash to one of the previous 5 non-polling calls | warn | 2.9% / 0.2% / 0% | Requiring the same output removes polling that is waiting for a change. Without it the rule fires on 27% of Codex sessions. Exclude polling tools by name. |
| `retry_loop` (after error) | 3 or more retries, within 5 calls, of a call whose result was an error | warn | 1.0% / 0% / n/a | Repeating a failed call unchanged is the clearest wasted work. Report it under the same code with a `cause` value. |
| `repeated_read` | One path read 5 or more times with the same range and no write to it between | info | 1.0% / 0% / n/a (inner 6.0%) | A path-only count fires on paging. Counting identical ranges, and resetting on a write, leaves rereads of an unchanged file. |
| `context_pressure` | 3 or more compactions | info | 23.6% / 0.2% / 29.5% | Compactions are the one context signal that means the same thing in every harness. Absolute peaks depend on the model window, which the events do not carry, and growth ratios fire on normal sessions. Add a peak rule only when the model window is known: 90% of it. |
| `context_pressure` (cache) | 3 or more cache-read collapses without a 5-minute idle gap, Claude Code only | info | 0% / 0.1% / not applied | Collapses after idle are cache expiry. Codex cached-input counts are too noisy to use. |
| `orphan_call` | 3 or more calls without a result, not counting a trailing call in the last turn | info | 1.0% / 0% / 0% | A single trailing orphan is an interrupted or live session. Disable for Cursor CLI until its normalizer orders blobs. |
| `orphan_result` | 1 or more results without a call, unless the session has a parent or was resumed | info | 11.8% / 0% / 0% before the exception | Most Claude Code orphan results are calls made in an earlier file. The exception has to be measured once the parent link is used. |
| `unreadable_lines` | 1 or more unreadable lines | error | 0.9% / 0% / 0% | Rare, and always a damaged transcript. |
| `noisy_tool_origin` (proposed) | One MCP server or extension holds 25% or more of a session's tool result bytes and 256 KiB or more | warn | 0% / 0% / 0%; Codex inner 10.7% | Separates a noisy server from normal use. The size floor keeps small sessions out. |
| `unknown_tool` (proposed) | A call to a tool name the harness did not offer | warn | 128 calls, every one failed | Cheap to detect from the error result, and it points at a wrong name in a prompt or configuration. |

The thresholds belong in the embedded suggestions file next to each code,
so a later calibration on a lake changes data, not code.

## Tool naming per harness

| Harness | Built-in tools | MCP tools | Extension or plugin tools |
|---|---|---|---|
| Claude Code | Bare names: `Bash`, `Read`, `Edit`, `Write`, `Agent`, `WebFetch` | `mcp__<server>__<tool>`; the server name keeps its hyphens | `mcp__plugin_<plugin>_<server>__<tool>`, from Claude Code's documentation. No such call is in the corpus. |
| Codex, normalized | Code mode: one `custom_tool_call` named `exec` whose input is a script. Other tools are `function_call` with a bare `name` and a separate `namespace` field (`collaboration`, `clock`, or none) that the normalizer keeps in `extra` only | Called from the script as `tools.mcp__<server>__<tool>`, with the server's hyphens turned into underscores. Never a normalized `tool.name` in code mode | `namespace` plays this role for Codex's own tool groups |
| Codex, inner calls | `item_completed` items: `CommandExecution`, `FileChange`, `ImageView`, `Extension` (`kind` such as `clock.sleep`), `CollabAgentToolCall` (`tool`) | `McpToolCall` with separate `server` (hyphens kept) and `tool` fields | `Extension.kind` |
| Codex, outside code mode | Not in the corpus. The normalizer handles `exec_command_begin`, `patch_apply_begin` and `function_call` names | `mcp_tool_call_begin` with `invocation.server` and `invocation.tool`. The normalizer sets `tool.name` to `invocation.tool` only and drops the server, so the server must come from `extra` | not seen |
| Cursor CLI | Bare names: `Shell`, `Read`, `Grep`, `Glob`, `StrReplace`, `Write`, `CreatePlan` | `CallDynamicTool` with `args.namespace = <server>`, `args.toolName = <tool>`, and `args.mcpDetails` on some calls. `GetDynamicTools` lists a namespace | The same `CallDynamicTool` with `namespace = cursor` for Cursor's own tools (`AwaitShell`, `TodoWrite`, `Task`, `SwitchMode`, `Delete`, `WebSearch`) |
| OpenCode | `read` in the local database, as a part `{type: tool, name, id, state}` | not seen | not seen |
| Cursor IDE, Grok, grokbot, terva | no local data | no local data | no local data |

The origin parser therefore needs per-harness rules: a name prefix for
Claude Code, the `CallDynamicTool` arguments for Cursor CLI, and the
item fields or script call names for Codex.

The local OpenCode database uses a newer schema (`session_v2` and
`session_message`, with tool parts keyed `name`, `id`, `state.content`
and `state.time.{created, ran, completed}`) than the `opencode export`
document the normalizer reads (`tool`, `callID`, `state.output`). Whether
`opencode export` from this version still emits the old shape was not
checked, because it reads the live database.

## Where each harness keeps a read path

| Harness | Read | Write |
|---|---|---|
| Claude Code | `Read.file_path`, with `offset` and `limit` for the range | `Edit.file_path`, `Write.file_path`, `MultiEdit.file_path`, `NotebookEdit.notebook_path` |
| Codex | No read tool. In inner calls, `CommandExecution.parsed_cmd[]` entries with `type = read` carry `path`; entries with `type = search` and `list_files` carry `path` too. In normalized events this is only in an `unknown` event | `FileChange.changes`, an object keyed by path; outside code mode, `*** Update File: <path>` headers in the `apply_patch` input |
| Cursor CLI | `Read.path`, with `offset` and `limit`. `Grep.path` and `Glob.target_directory` name search roots | `Write.path`, `StrReplace.path` |
| OpenCode | `read` `state.input.path` in the local database | not seen |

Shell reads (`cat`, `sed -n`, `head`, `tail`) are common in every
harness. In Claude Code, 5,074 `Bash` calls matched a simple read
pattern, against 6,094 `Read` calls. Parsing shell commands is a
heuristic; the defaults above use structured fields only, and Codex's
`parsed_cmd` is the one structured parse of a shell read available.

## Latency feasibility

| Harness | Source | Results with both times | p50 | p95 | p99 |
|---|---|---:|---:|---:|---:|
| Claude Code, all | line `timestamp` of the call and of the result | 100% | 59 ms | 20.2 s | 302 s |
| Claude Code, `Read` | same | 100% | 10 ms | 174 ms | 262 ms |
| Claude Code, `Bash` | same | 100% | 80 ms | 24.7 s | 345 s |
| Claude Code, MCP | same | 100% | 111 ms | 4.2 s | 20.0 s |
| Codex, all cells | line `timestamp` of `custom_tool_call` and its output | 100% | 306 ms | 20.0 s | 45.0 s |
| Codex, `exec` cells | same | 100% | 343 ms | 5.2 s | 11.0 s |
| Codex inner, MCP | `McpToolCall.duration` | 100% | 119 ms | 2.5 s | 13.8 s |
| Codex inner, commands | `CommandExecution.duration` | 100% present, 58% zero | - | - | - |
| Cursor CLI | none | 0% | - | - | - |
| OpenCode | `state.time` in the source | not measured | - | - | - |

- **Claude Code** supports latency for every call. The figure includes
  time waiting for a person: `AskUserQuestion` has a p50 of 90 s, and a
  `Bash` call waits for permission prompts. Report latency per tool, and
  leave out tools that wait on a person.
- **Codex** supports latency at the cell level. Cell latency clusters at
  the 5 s yield limit, so a cell's latency is not one tool's latency. For
  inner MCP calls `duration` is good. Inner command `duration` is zero
  for 58% of commands, so it cannot be used as is.
- **Cursor CLI** parts carry no timestamps, so latency is not possible
  from the store.
- **OpenCode** keeps `created`, `ran` and `completed` per tool part, but
  the normalizer gives the call and the result the same `recorded_at`
  (the first of `created`, `start`, `end`, `updated`) and drops
  `state.time` from `extra`. Latency needs the end time kept.

## Gaps found in the normalized data

These change the plan for the epic's normalizer tickets:

1. **Codex code mode hides every real tool call.** The normalizer
   projects `item_completed` as `unknown`, so in code mode the only
   normalized tool is `exec`, with no MCP server, no command, no exit
   code and no path. Projecting `McpToolCall`, `CommandExecution` and
   `FileChange` items as tool events (marked as inner calls of the cell)
   is what makes Codex usable for tool statistics.
2. **Codex `exec` results carry no error flag.** `custom_tool_call_output`
   has no `is_error`; errors live in the inner items' exit codes.
3. **Codex MCP calls outside code mode lose the server** in `tool.name`.
4. **Cursor CLI emits no tool events.** Its `tool-call` and `tool-result`
   parts stay in `unknown` events, and the blob order is not the
   conversation order.
5. **OpenCode loses the tool end time**, and the local database schema
   differs from the export the normalizer reads.
6. Claude Code results carry no tool name, as the epic records. Joining
   on `call_id` within the file works: 16 results found no call, and 17
   of 166,831 calls found no result.
7. The epic says OpenCode leaves `is_error` null. The normalizer sets it
   from `state.status` (`completed` is false, `error` is true).
