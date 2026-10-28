---
schema: 3
id: TKT-01M61MZNMZ0ZC0CG223990GMCH
title: Session diagnosis engine with coded findings and suggestions
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
assignees: []
milestone: null
parent: TKT-01M61MYZ6H6J8MGNPWHS8MTJ1X
origin: null
dependencies:
  - TKT-01M61MZN03D4QZ6CAJPRNDFM6S
  - TKT-01M61MZMR6EHF8BAC5F4VNK9VN
  - TKT-01M61MZMXHF57GKYYRE9ZQ4S3S
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T16:20:10Z
updated_at: 2026-10-28T19:29:51Z
created_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
extensions: {}
---

## Description

A streaming pass over one session's events file that returns findings, each with a stable code, a severity, the event positions involved, the measured values, and a suggestion.

Initial codes, with thresholds from the calibration spike: `large_tool_result`, `large_tool_input`, `error_streak`, `retry_loop` (same tool and input hash), `repeated_read` (needs a per-harness parser of each harness's tool inputs for the path), `context_pressure` (input token growth, compactions, cache-read share), `orphan_call`, `orphan_result`, `unreadable_lines`.

Suggestions are static text keyed by code, in an embedded file versioned like internal/advisory/agents.json, so they are reviewable and change with a release. Hash inputs; never return content. Memory stays bounded regardless of session size.

## Acceptance criteria

- [x] One streaming pass returns findings with code, severity, positions, values and suggestion.
- [x] The initial codes are implemented with thresholds from the calibration spike.
- [x] Suggestions live in an embedded, versioned file keyed by code.
- [x] Memory stays bounded on the large synthetic transcripts from the performance workloads.

## Implementation plan

As built (refines the coordinator's plan):

- Package internal/diagnose. `New(rules) *Analyzer`, `(*Analyzer).Line(line []byte)` per events-file line in order (nil = oversized line), `(*Analyzer).Report() Report`. The pass keeps capped tables only: open calls (4096, oldest forgotten and remembered as closed), recently closed ids (4096), a 5-call retry window, path+range read counts (4096), per-tool / per-origin entries (256 names), MaxPositions = 16 positions per finding.
- Rules and suggestions in internal/diagnose/suggestions.json (go:embed, version 1): per code severity, exact threshold keys, suggestion, optional https link, skip_harnesses; plus polling_tools. Parse refuses a missing or unknown code, a wrong threshold key set, a non-positive threshold, a bad severity, an empty suggestion.
- recall: `(*Reader).Diagnose(ctx, scope, uid) (diagnose.Report, error)`: inScope (ErrNotFound), open (pins a generation; UnavailableError / ErrGenerationChanged), snap.lines from 0, readLine(MaxLine), fills session_uid / generation / head_sha256.
- Tests: one fixture per code with negatives, a Claude transcript through normalize.Claude, a parse/one-to-one test on the shipped file, a 1M-event bounded-heap test and a flat allocs-per-line test, and a recall test for scope, unknown and unavailable sessions.

## Notes

**agent:claude-code/049834bc-diag** at 2026-10-28T17:06:50Z

Decisions and deviations from the plan, with reasons.

### Entry point

- The plan's `Diagnose(ctx, lines iter, Options)` became a push API, `New(rules).Line(b)` then `Report()`. The line reader with the MaxLine cap (readLine) lives in recall, and recall imports diagnose, so diagnose cannot reuse it. A push API also lets a later lake-wide walk feed lines from any reader. `Options` is just `*Rules`; nil means Shipped.
- `Reader.Diagnose(ctx, scope, uid)` takes no pinned-generation argument. Each call opens the published generation and reports which one it read (generation, head_sha256), and positions are only meaningful for that generation. A surface that needs `?gen=` can compare the report's generation itself.

### Rules

- The thresholds sit in suggestions.json next to each code, as the calibration doc asks. A later calibration then changes data, not code. Each code's set of threshold keys is fixed in Go (thresholdKeys), and Parse refuses any other set.
- Polling tools are a list in the same file: Codex sleep, wait, wait_agent, list_agents, clock.sleep, collab:wait; Claude Code Monitor, TaskStop, ListAgents; Cursor CLI AwaitShell, GetDynamicTools.
- orphan_call and orphan_result are both off for cursor-cli through skip_harnesses. The ticket names only orphan_call, but the doc calls every Cursor CLI orphan likely an ordering artefact, and 7 of 8 sessions had orphan results. Remove cursor-cli from the list once its normalizer orders blobs.

### Per-code choices

- **large_tool_result**: one finding. Its positions are the 64 KiB results when the single-result rule fired, otherwise the 32 KiB ones. Codex inner results are excluded.
- **error_streak**: a result counts as an error only when is_error is true, and anything else resets the streak (a missing flag means no error in Claude Code). Codex inner results count, because their exit codes are the only Codex error signal. Positions are where each qualifying streak starts.
- **retry_loop**: model-made calls only, not Codex inner calls. A call is a retry when tool and sha256(input) match one of the previous 5 non-polling calls. The finding is per tool, with cause after_error when the earlier call failed and same_output when the result hashes are equal. A retry of a failed call counts as after_error only.
- **repeated_read**: reads the structured fields recorded by the spike: Claude Read file_path+offset+limit, Cursor CLI Read path+offset+limit, and OpenCode read filePath/path. A write resets the count: Claude Edit/Write/MultiEdit/NotebookEdit, Cursor Write/StrReplace, OpenCode write/edit, and Codex apply_patch file headers. Paths and ranges are hashed and never returned. Codex code-mode reads (`CommandExecution.parsed_cmd`) and `FileChange.changes` are not read yet, because the inner projection is not on this branch.
- **context_pressure**: two causes. compactions counts 3 or more compaction runs; consecutive compaction events with no tool event between them are one compaction, since Claude Code writes a boundary and a summary and Codex writes a response item and an event message. cache_collapse is Claude Code only, needs 3 or more, skips any drop after an idle gap of 300 s or more, and treats a missing timestamp as idle. Identical consecutive usage events count once. peak_context_tokens is reported as a value only, because the model window is not in the events.
- **orphan_call**: calls still open at the end. A call after the last tool result (a trailing call) does not count. Codex's doubled calls under one id are handled by a pending count.
- **orphan_result**: suppressed when the session has parent_session_id, or when it was resumed. "Resumed" means an orphan result arrived before any call; this is a heuristic until the parent/resume link is used. Results for recently closed or forgotten ids are not orphans.
- **unreadable_lines**: counts the normalizer's unreadable markers (an error event whose extra has unreadable_line) and lines of the events file that do not decode. A line over MaxLine is not damage: it is reported as oversized_events on the report, not as a finding.
- **noisy_tool_origin**: implemented. The origin comes from a local parse of `mcp__<server>__<tool>` (also with a `tools.` prefix for Codex scripts), and from Cursor CLI CallDynamicTool's namespace or args.namespace, where the namespace `cursor` is built-in. Share and the 256 KiB floor are measured per pool: model-visible results, and Codex inner results (cause inner_calls). **Switch to recall.ToolOrigin when it lands.** diagnose is imported by recall and so cannot import it; the classifier should move to a leaf package (for example internal/toolorigin) that both import.
- **unknown_tool**: implemented for Claude Code only. It matches an error result whose text contains "No such tool available". The text is matched inside the pass and never returned. Other harnesses are skipped: the offered tool list is not in the normalized events.

### Codex inner calls

The raw_type is one constant, `diagnose.InnerRawType = "item_completed"`, matched as a prefix on harness codex. **The coordinator should confirm this against the Codex inner-call normalizer change.** Inner calls and results are excluded from large_tool_result, large_tool_input, retry_loop, repeated_read and the orphan rules. They are included in error_streak and, in their own pool, in noisy_tool_origin.

### Memory test

internal/synthetic and cmd/lampi-perf are test-only or `package main`, so they cannot be imported. memory_test.go therefore generates a 1,000,000-event transcript in the perf workloads' shape (filler text messages), with hostile tool traffic added: unanswered calls, orphan results, a new path and a new MCP server per call, usage, compactions and errors. Heap after GC grew 30 KiB between 100k and 1M events, under a 4 MiB limit. Allocations per line stayed flat (15 early, 14 late).

**agent:claude-code/049834bc** at 2026-10-28T17:51:46Z

Coordinator: diagnose now reads origins through internal/toolorigin (TKT-01M61MZN3) instead of its local parse, and matches Codex inner calls on raw_type == item_completed exactly (TKT-01M61PSX contract). The old parse's fallbacks for unprojected Cursor CLI CallDynamicTool and Codex tools.-prefixed names are gone: both normalizers now emit mcp__<server>__<tool>, so old projections need serve normalize --all, which the release already requires.

**agent:claude-code/049834bc** at 2026-10-28T18:00:28Z

Cursor CLI events can now carry extra.order = unestablished (PR #220 fix 5780868): their place among older compaction branches is a guess. Sequence rules (retry_loop, error_streak, repeated_read) should skip or reset at those events; orphan rules are already off for Cursor CLI. To do with the diagnosis surfaces work.

**agent:claude-code/049834bc** at 2026-10-28T18:03:56Z

Review 2903 on PR #222 (two medium findings), fixed in 6fc2cc8. (1) retry_loop summed repeats per tool across the session; it now counts loops, meaning chains of repeats each within the window of the previous one that reach the threshold. Values are loops and retries (the longest chain), with one position per loop. A negative test covers separated pairs with their own inputs, and another covers two loops. (2) Compaction dedupe was reset only by tool events; message events now reset it too. A test covers three compactions in a tool-free Codex conversation, with usage between one compaction's two records. Also done here: events with extra.order unestablished are skipped by error_streak, retry_loop and repeated_read.

**agent:claude-code/049834bc** at 2026-10-28T18:22:20Z

Review 2904 on PR #222 (two medium findings) fixed in 7cc733e. (1) A doubled Codex call entered the retry window before its id check; the id is now checked first, and the doubled-call fixture asserts a retry_loop over four doubled calls. (2) Retry chains were compared only when results arrived in call order; calls now settle when both results are in, in either order, and wake the repeat waiting on them. A reverse-order fixture asserts the loop. Both new assertions fail on the previous code.

**agent:claude-code/049834bc** at 2026-10-28T18:28:39Z

Review 2906 on PR #222: finding-1 (doubled Codex calls in the retry window) was already fixed in 7cc733e. finding-2, a duplicate usage that did not refresh the gap time, is fixed in 0996390, with a repeated-baseline fixture in TestContextPressureCacheCollapse that fails on the previous code.

**agent:claude-code/049834bc** at 2026-10-28T19:01:41Z

Review 2911 on PR #222 (medium) fixed in 3c19874: eviction from the retry window no longer clears a pending call's link. A test with four repeats separated by four filler calls, results delayed and in reverse, now finds the loop; it fails on the previous code. Memory stays bounded: links join only unsettled calls, which maxOpenCalls caps, and the 1M-event allocation test still passes.

**agent:claude-code/049834bc** at 2026-10-28T19:18:36Z

Review 2913 on PR #222 (high) fixed in b442177. My previous claim that maxOpenCalls bounded retry links was wrong, because closing a call never cut them. Now: a ring of at most maxWaiting=1024 repeats linked to unsettled calls (the oldest is cut loose and becomes a chain root); calls forgotten before their result are abandoned and wake their waiter; a call without a result extends no chain. TestMemoryBoundedRepeats (identical calls never answered; the first never answered and the rest answered) shows a flat heap over 500k calls, against 62 MiB of growth on the previous code. The spanning-window and reverse-order retry tests still pass.

**agent:claude-code/049834bc** at 2026-10-28T19:26:03Z

Review 2915 on PR #222 (medium) fixed in 3715c51: the polling exclusion now matches Cursor CLI tools under both the bare name and the older mcp__cursor__ name (pollingName). TestRetryLoop fails on the old check with a retry_loop for mcp__cursor__AwaitShell.

**agent:claude-code/049834bc** at 2026-10-28T19:29:51Z

Review 2916 on PR #222 (medium) fixed in e14b7bd: toolCall resolves the Cursor CLI built-in name once (builtinName strips mcp__cursor__) and uses it for the polling list and the read and write tables, so the same name gap cannot recur rule by rule. TestRepeatedRead runs reads, a StrReplace and a fifth read under both names; without the resolution the mcp__cursor__ case finds no repeated_read.

## Summary

internal/diagnose holds the engine: Analyzer (New / Line / Report), the Report, Finding and Suggestion types with JSON tags, and suggestions.json (version 1), which carries each code's severity, thresholds, suggestion and skip_harnesses, plus the polling tools. (*recall.Reader).Diagnose(ctx, scope, uid) pins a generation, streams the events file and returns the report with session_uid, generation and head_sha256. A session outside scope is ErrNotFound. All 11 codes are implemented with the calibration doc's defaults; unknown_tool covers Claude Code only. The deviations, the open points and the bounded-memory evidence are in the note. Two open points: confirm the Codex inner raw_type (InnerRawType = item_completed), and move origin parsing to a leaf package shared with recall.ToolOrigin. GOFLAGS=-mod=mod just ci passes. Surfaces are left to TKT-01M61MZNR.
