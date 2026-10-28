---
schema: 3
id: TKT-01M61MZNRCQC8MHFF7RHT1J293
title: "CLI, API, MCP and dashboard: diagnose a session"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/docs
assignees: []
milestone: null
parent: TKT-01M61MYZ6H6J8MGNPWHS8MTJ1X
origin: null
dependencies:
  - TKT-01M61MZNMZ0ZC0CG223990GMCH
  - TKT-01M61MZNFY69TNQJWH0PS2WPKS
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T16:20:10Z
updated_at: 2026-10-28T19:58:13Z
created_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
extensions: {}
---

## Description

Expose session diagnosis:

- CLI: `terva-lampi query diagnose SESSION`, and a lake-wide mode (`--since 7d`) that groups findings by code, harness and project.
- Read and browser API routes for one session's findings.
- MCP: a `diagnose_session` recall tool, so an agent can review its own past sessions.
- Dashboard: a findings panel on the session page, each finding linking to its events and showing its suggestion.

Decide whether findings are computed on request or cached per session generation; the lake-wide mode is the case that may need caching.

## Acceptance criteria

- [x] terva-lampi query diagnose answers one session and a lake-wide grouped view.
- [x] API routes and a diagnose_session MCP tool answer the same requests.
- [x] The session page shows findings with links to their events and their suggestions.
- [x] The compute-on-request versus cache decision is recorded with its reasons.

## Implementation plan

- recall: `DiagnoseLake(ctx, DiagnoseLakeRequest)` walks the most recently updated ready sessions in scope (catalog `RecentReadySessions`, newest last head update first, with since, harness and project filters, reading limit+1 to know there are more). It runs `Reader.Diagnose` on each and groups findings by code × harness × project_id: sessions, findings, events, and up to 3 example session uids. The limit defaults to 200, at most 1000. The walk stops 500 ms before the read context's deadline and reports `truncated` with reason `time`; more sessions than the limit is reason `limit`. A session purged, normalized again or no longer ready between listing and reading counts in `skipped`.
- web (internal/web/diagnosis.go): browser `GET /api/web/v1/sessions/{uid}/diagnosis` and `GET /api/web/v1/diagnosis`; read-token `GET /api/read/v1/sessions/{uid}/diagnosis` and `GET /api/read/v1/diagnosis` (events:read, token scope, audit before answering, no-store). The read-token prologue of readTools moves into one helper the three read routes share. The single-session body is the engine's Report with `links` per finding, parallel to `positions`: relative (EventLink) on the HTTP routes and absolute on MCP, as tool_stats does.
- MCP: `diagnose_session` (session_uid) and `diagnose_lake` (since, limit, harness, project), with the routes' bodies.
- CLI: `terva-lampi query diagnose SESSION_UID` (a summary line, then one finding line each) and `terva-lampi query diagnose [--since D] [--limit N] [--harness H] [--project ID]` (a summary line, then one group line each). The fetch and --out reuse the query tools helpers.
- Dashboard: a closed-by-default findings panel on the transcript page, computed on request under its own budget inside the page's read context. A failure or timeout leaves a one-line notice and the page still renders.
- Decision: compute on request; the lake-wide view is a capped walk; a per-generation cache is deferred until measured.
- Docs: cli.md, web-api.md (Session diagnosis, MCP table), reading-the-lake.md, web-dashboard.md, performance.md (measurements).

## Notes

**agent:claude-code/049834bc-dsurf** at 2026-10-28T18:45:11Z

Decisions, with reasons.

### Compute on request, no cache

Findings are computed on request on every surface: the one-session routes, the MCP tools, the lake-wide view and the transcript panel. One session is one pass over its events file. TestDiagnoseScale measured 1.38 to 1.44 s for 200,000 events of 1 KiB, about 140,000 events a second, so a session of up to about 700,000 such events answers within the 5 s read context. An answer computed on request always matches the current generation and needs no invalidation.

The lake-wide view is a capped walk, not a cache. It reads at most `limit` sessions (default 200, at most 1000), newest last head update first (catalog RecentReadySessions, on the web_sessions_* indexes). It stops 500 ms before the read deadline and reports `truncated_by: time`; more sessions than the limit is `truncated_by: limit`. 200 sessions of 2,000 events took 3.32 to 3.47 s. A lake whose recent sessions average much over 2,500 events returns a partial, truncated answer rather than a timeout.

A per-generation cache lost for now. It would need invalidation on every publish, a store (a catalog table or the index) and a migration, and it only pays off for the lake-wide view. It is deferred until the walk is measured too slow on a real lake. The numbers are in docs/performance.md, "Session diagnosis".

### Surfaces

- The JSON body is the engine's Report, with `links` added to each finding, one per position in the same order (recall.EventLink). They are relative on the HTTP routes and absolute through MCP, as tool_stats does with `link`. A parallel array keeps `positions` as the engine reports it and needs no new type in internal/diagnose.
- The one-session routes take no parameters, and refuse any (`invalid_request`), so `gen` is not silently ignored. The report names the generation it read.
- Lake-wide groups are keyed by code, catalog harness and project_id, not by cause or tool. They carry sessions (sessions with the code), findings (a session can have one per tool), events (counts summed), up to 3 example uids, and the suggestion. Severity rank was exported from internal/diagnose (Severity.Rank) to sort groups.
- `diagnose_lake` was added beside `diagnose_session`: it is the same function as the route, so it cost one schema entry.
- The read-token prologue of readTools (token, no-store, parse, audit, touch, scope) moved into readTokenAnswer, which the tool statistics route and both diagnosis read routes share. The tool statistics audit detail is unchanged (`tools query=...`). Diagnosis audit lines are `diagnosis session=UID query=` and `diagnosis query=...`.
- The transcript panel runs under its own 1 s budget inside the page's 5 s read context, so a long session costs each of its pages at most a second, then shows a notice and the page renders. A generation published between the page read and the diagnosis also shows a notice instead of links into the wrong generation. The panel is a closed details element above the events; its summary counts findings per severity.
- CLI: `query diagnose UID` and lake-wide without a uid (since defaults to 7 days on the lake). The uid can come before or after the flags: flags are parsed again after it, each defaulting to the value already read. The fetch and the --out writing are shared with query tools (fetchRead, writeQueryOut).

### Checks

- GOFLAGS=-mod=mod just ci passes (XDG_STATE_HOME and XDG_DATA_HOME in scratch; XDG_CONFIG_HOME could not be set in this session).
- Browser smoke with the Playwright 1.63 module already in the npm cache and its installed Chromium 1243 (no install): the new diagnosis step passed (panel opened on a real retry_loop finding, its event link focused the event). The smoke then fails later at the search page's coverage line, `Searching N of M ready sessions`; the unchanged script from before this work fails at the same line, so it predates this ticket (possibly the Playwright version, the README pins 1.58.2).

**agent:claude-code/049834bc** at 2026-10-28T19:58:13Z

Review 2917 on PR #224 (two medium) fixed in 2340d77. (1) ParseWhen refuses times outside UnixNano's range (about 1678 to 2262). This covers every date filter, including tool statistics, which had the same wrap. Before the fix, since=2500-01-01 returned a current session and 1500-01-01 returned none. (2) DiagnoseLake skips a session whose diagnosed generation differs from the listed one (walkLake, TestDiagnoseLakeSkipsRepublished). Both tests fail with the checks removed.

## Summary

Session diagnosis is exposed on every surface, computed on request. GET /api/web/v1/sessions/{uid}/diagnosis and /api/read/v1/sessions/{uid}/diagnosis (events:read, token scope, audited, no-store) return the engine's Report with a viewer link per position, absolute through the MCP diagnose_session tool. GET /api/web/v1/diagnosis and /api/read/v1/diagnosis, and MCP diagnose_lake, walk at most limit (200, max 1000) of the most recently updated ready sessions since `since` (7 days), group findings by code, harness and project with example uids, and report truncated (limit or time; the walk stops 500 ms before the read deadline). terva-lampi query diagnose writes either as JSONL. The transcript page has a closed findings panel with severity, code, values, suggestion and event links, under a 1 s budget, with a notice on timeout or a generation change. Docs: web-api.md (Session diagnosis, MCP rows), cli.md, reading-the-lake.md, web-dashboard.md, performance.md (1.4 s for 200,000 events; 3.4 s for 200 sessions of 2,000). The decision and its reasons are in the note: no cache until the walk is measured too slow on a real lake. just ci passes; the browser smoke passes the new step and fails later at a search check that already failed before this work.
