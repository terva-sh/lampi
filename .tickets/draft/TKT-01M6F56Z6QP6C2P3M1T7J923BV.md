---
schema: 3
id: TKT-01M6F56Z6QP6C2P3M1T7J923BV
title: "MCP: discover sessions and bays for lake triage"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
  - area/server
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-11-02T22:13:54Z
updated_at: 2026-11-02T22:15:33Z
created_by:
  id: agent:codex/t3-6387cc8a
  name: ""
updated_by:
  id: agent:codex/t3-6387cc8a
  name: ""
extensions: {}
---

## Description

### Problem

The owner wants to dogfood the lake through its agent tools and separate recently ingested code-review sessions from project-development sessions. The current six MCP tools search events, read events, copy excerpts, summarize tool usage, and diagnose sessions or the lake. None lists bays, projects, or sessions, exposes session routing metadata, or selects a bay within a broader read-token scope. An agent cannot discover project IDs or inspect the grouping it wants to sort without transcript searches, browser authentication, or lake-host access.

### Existing surfaces and proposed scope

Build read-only discovery over the existing authorized catalog/query layer, available through MCP and an appropriate CLI surface. Consider listing visible bays and projects, paginated session enumeration, and a session metadata read that supplies permitted working-directory, remote, harness, current bay and origin evidence. Define metadata exposure explicitly for events:read; do not assume it grants raw-artifact access. Bay selection must intersect the caller's current scope, and summaries/counts must reveal only reachable sessions. A token can already be scoped to a bay, but selecting one bay within a multi-bay token is a separate usability need.

Keep classification, membership writes, grants and profile changes outside this ticket. The first dogfooding query should identify candidate review sessions and enough evidence for a later reviewed migration preview.

### Inspection evidence and decisions

Reviewed docs/reading-the-lake.md, internal/web/mcp.go, internal/cli/mcp.go, internal/config/profile.go, internal/config/bays.go, internal/protocol/protocol.go, internal/catalog/routing.go and internal/catalog/inbox.go on 2026-11-02 at repository HEAD 9e0ffba. MCP search returns event hits with session/project IDs and display labels, but no bay or working-directory routing metadata. All six tools apply the read token's current scope and audit their calls.

A profile controls capture configuration and does not carry bay requests. Client bay requests live in each machine's lake config; lake routing rules separately match working-directory fields, Git remotes and harness. Existing rules can separate review sessions if their import has a distinguishing metadata pattern. Purpose/content matching is not implemented, so do not classify every session mentioning review as a code review, or route an entire development repository merely because some sessions are reviews. Bulk move accepts project/directory/remote/device/harness filters, but no explicit session list. Lake add rules only add membership; moving existing sessions out of default is a separate operation.

Local read-only inspection found an active lake at the configured loopback address, a healthy health endpoint, and an agent consuming the default profile with 75 allow rules. No events:read token is configured, and lake-host data requires another account's permissions. A private read-token request is pending. No live session counts, review classifications, current bay contents or migration preview have been verified. No live configuration, token, profile or membership was changed. The working tree was clean before inspection.

### Alternatives

Using transcript search to infer an inventory loses sessions with no matching text and returns event hits rather than a complete session listing. Exporting every normalized event is available through query events, but scanning transcript content is excessive for metadata discovery. Browser or privileged catalog access can help an operator but does not dogfood the agent interface. Adding administrative MCP writes before read-only discovery would not solve the missing evidence for a precise selection.

## Acceptance criteria

- [ ] An events:read client can page through visible bays, projects and sessions without searching transcript text or using a browser login.
- [ ] Session metadata exposes the permitted routing evidence needed to distinguish imported reviews from development, without exposing raw artifacts or hidden bay membership.
- [ ] Recall can select a visible bay while retaining the token scope; hidden bays, sessions and project metadata cannot be enumerated.
- [ ] CLI and MCP discovery share authorized query logic, with paging and scope tests and documented dogfooding examples.

## Notes

**agent:codex/t3-6387cc8a** at 2026-11-02T22:15:33Z

Supersedes the description's pending-token inspection state: the owner supplied a read token through the private secret card, and all six MCP tools succeeded through the installed terva-lampi stdio bridge against the configured local lake. The private credential file was read by the bridge; no value was printed or recorded. Initialization and tools/list confirmed the six advertised schemas. Search coverage at 2026-11-02T22:14:21Z reported ready_sessions=2237, indexed_sessions=2237, behind_sessions=0 and failed_sessions=0. This is the index's coverage report, not a fresh catalog inventory of every session.

The owner clarified that the intended reviews were Vuoro reviews on Claude Code, rather than automated Forgejo reviews. Read_events inspected the opening events of sessions 01M6F532E20X2S9YFJQP9ZR4TR and 01M6F4Z4BV9SWF5TXE375T7XQ0: their first user prompt asks to review local commits. Companion 01M6F55Q2DJCFV57PGW0WFE87W asks to check findings and mark each with rate_draft. The first reviewer and companion share cwd_hash 1acee502fac9a9ab and the search label /home/sothr/.local/state/vuoro/worktrees/20261102-221134-b61304; the second reviewer has cwd_hash f882747de0e6661a and a neighboring timestamped worktree. Their normalized extra fields identify sdk-cli/sdk origins, which alone are too broad to establish review purpose. The companion has no project_id, so grouping solely by development project misses it. Project labels can also be remotes instead of paths, reinforcing the need for explicit session metadata.

Copy_excerpt, tool_stats, diagnose_session and a bounded diagnose_lake call also succeeded. The bounded lake diagnosis correctly reported truncation; it is not a complete diagnosis of the lake. Literal user-message searches for vuoro and review each filled a 200-hit page with continuation, so neither sample is a complete review inventory. Search text is a useful probe, not the proposed move selector. No catalog access or membership/profile/grant/configuration changes were made.
