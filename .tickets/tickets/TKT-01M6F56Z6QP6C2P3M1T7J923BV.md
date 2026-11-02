---
schema: 3
id: TKT-01M6F56Z6QP6C2P3M1T7J923BV
title: "MCP and CLI: discover bays, projects and session metadata"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
  - area/server
assignees: []
milestone: null
parent: TKT-01M6F5K38C9N9Y1TRX4PZB7JSW
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:codex/t3-6387cc8a
  branch: t3/split-code-reviews-bay
  worktree: /home/sothr/.t3/worktrees/lampi/t3-6387cc8a
  commit: 58b3063e8e69b65967be973a14793101a8416d8d
  session: null
  claimed_at: 2026-11-02T22:57:43Z
  expires_at: null
archive: null
created_at: 2026-11-02T22:13:54Z
updated_at: 2026-11-02T22:57:43Z
created_by:
  id: agent:codex/t3-6387cc8a
  name: ""
updated_by:
  id: agent:codex/t3-6387cc8a
  name: ""
extensions: {}
---

## Description

### Problem and current evidence

The owner wants to dogfood the lake through agent tools and identify Vuoro code-review sessions separately from project-development work. All six existing MCP tools succeeded against the local lake on 2026-11-02, as recorded in the inspection note. They expose event recall, excerpts, tool usage and diagnosis, but do not enumerate bays, projects or sessions or provide a session metadata read. An agent currently has to infer an inventory from transcript searches, a browser or privileged catalog access.

Vuoro samples include reviewers and finding-rating companions sharing review worktrees. Companions may have no project ID; project labels can be paths or remotes. These observations require explicit directory/remote/provenance fields and stable session IDs rather than guessing from display labels. The first inspection's pending-token state was resolved by the successful tool calls documented in Notes. No complete review cohort, live bay inventory or migration preview has been obtained, and no live configuration or membership was changed.

### Scope

Build read-only discovery over shared authorized catalog/query logic, available through MCP and remote CLI: visible bay and project listings, paginated session enumeration and one-session metadata. Supply permitted working-directory, remote, harness, current visible memberships and available origin/provenance evidence; distinguish missing values from unknown or unavailable data. Do not invent provenance that capture does not record.

Define metadata exposure explicitly for events:read. Discovery must intersect current session/bay scope and reveal neither hidden bay names nor unrelated project/device information. Counts and pagination reflect reachable sessions, and all continuations are bounded and scoped. Raw-artifact permission and mutation authority are separate. Document stable schemas, errors and incomplete coverage.

### Related work and rationale

TKT-01M6F5K429GX4TZFJB35TV3KC7 (MCP and CLI: filter recall and analysis by selected bays) owns bay selection within recall and analysis. TKT-01M6F5K49E9JCCE6C80FTTNRP3 (MCP: preview session selections and bay migration plans) owns selection refinement and membership previews. This discovery ticket provides their metadata without adding administrative writes or a classifier.

Transcript search returns event hits, omits sessions with no matching text and does not establish a complete session inventory. Exporting every normalized event can scan excessive content for a metadata question. A browser or a direct catalog query helps an operator but does not exercise the agent interface. Build authorized discovery first and preserve source/session identity while regrouping.

## Acceptance criteria

- [ ] An events:read client can page through visible bays, projects and sessions and inspect one session without transcript-text searches or browser authentication.
- [ ] Metadata exposes permitted routing evidence including explicit directory/remote fields, harness, current visible memberships and available provenance, with absence/unknown distinguished.
- [ ] Scope, counts and cursor validation prevent enumerating hidden bays, sessions, projects or device metadata; test both session-scoped and bay-scoped tokens.
- [ ] CLI and MCP discovery use shared bounded queries with documented paging, machine-readable results, continuation and incomplete-coverage behavior.
- [ ] Document the exact metadata permission contract and tool schemas; no raw artifacts or new mutation authority are implied by events:read.

## Notes

**agent:codex/t3-6387cc8a** at 2026-11-02T22:15:33Z

Supersedes the description's pending-token inspection state: the owner supplied a read token through the private secret card, and all six MCP tools succeeded through the installed terva-lampi stdio bridge against the configured local lake. The private credential file was read by the bridge; no value was printed or recorded. Initialization and tools/list confirmed the six advertised schemas. Search coverage at 2026-11-02T22:14:21Z reported ready_sessions=2237, indexed_sessions=2237, behind_sessions=0 and failed_sessions=0. This is the index's coverage report, not a fresh catalog inventory of every session.

The owner clarified that the intended reviews were Vuoro reviews on Claude Code, rather than automated Forgejo reviews. Read_events inspected the opening events of sessions 01M6F532E20X2S9YFJQP9ZR4TR and 01M6F4Z4BV9SWF5TXE375T7XQ0: their first user prompt asks to review local commits. Companion 01M6F55Q2DJCFV57PGW0WFE87W asks to check findings and mark each with rate_draft. The first reviewer and companion share cwd_hash 1acee502fac9a9ab and the search label /home/sothr/.local/state/vuoro/worktrees/20261102-221134-b61304; the second reviewer has cwd_hash f882747de0e6661a and a neighboring timestamped worktree. Their normalized extra fields identify sdk-cli/sdk origins, which alone are too broad to establish review purpose. The companion has no project_id, so grouping solely by development project misses it. Project labels can also be remotes instead of paths, reinforcing the need for explicit session metadata.

Copy_excerpt, tool_stats, diagnose_session and a bounded diagnose_lake call also succeeded. The bounded lake diagnosis correctly reported truncation; it is not a complete diagnosis of the lake. Literal user-message searches for vuoro and review each filled a 200-hit page with continuation, so neither sample is a complete review inventory. Search text is a useful probe, not the proposed move selector. No catalog access or membership/profile/grant/configuration changes were made.

**agent:codex/t3-6387cc8a** at 2026-11-02T22:20:32Z

The owner requested a full tooling/UX/documentation backlog on 2026-11-02. Narrow this ticket to read-only discovery and routing evidence. Bay-filter implementation is split into a child of the same epic; its replacement acceptance criterion supersedes the original bay-filter criterion. Historical inspection notes remain evidence, including the successful six-tool dogfood and the pending-token statement they explicitly superseded. Listing visible bay names must respect the caller's discovery contract and reachable scope, rather than leaking every bay granted by unrelated credentials.

**agent:codex/t3-6387cc8a** at 2026-11-02T22:20:37Z

Bay filters are now tracked in TKT-01M6F5K429GX4TZFJB35TV3KC7 (MCP and CLI: filter recall and analysis by selected bays); selection previews are in TKT-01M6F5K49E9JCCE6C80FTTNRP3 (MCP: preview session selections and bay migration plans). Discovery stays focused on the metadata these tools need.
