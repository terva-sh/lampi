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
updated_at: 2026-11-02T22:13:54Z
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
