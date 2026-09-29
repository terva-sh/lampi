---
schema: 3
id: TKT-01M3N8KHW56GVZXHV0KBNSPEX1
title: "Bays: segment one lake and route sessions to a bay"
type: epic
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/catalog
  - area/agent
  - area/auth
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T00:21:33Z
updated_at: 2026-09-29T01:24:39Z
created_by:
  id: agent:claude-code/7859b064
  name: ""
updated_by:
  id: agent:claude-code/7859b064
  name: ""
extensions: {}
---

## Description

### Idea

One lake can be split into named **bays**. Each session is sent to one bay: the default bay, which every lake has, or a bay a user created. Bays let one lake hold work that should be kept apart, such as work and personal sessions, or one client's projects, without running a second lake.

The name comes from the lake metaphor. A bay is part of the lake and shares its water, so it is not a second lake. `docs/naming.md` should record the choice. "Pond" was the first word for this and was dropped because lampi already means pond. "Partition" was rejected because the repository uses it for the parquet partitions, and "space" and "collection" because they already appear in the code and docs.

### How this differs from many lakes

TKT-01M3FHHBC (Agent onboarding: registration codes, lake config, many lakes) already lets one agent send different sessions to different lakes, each with its own token, allowlist and state. That gives separation by running a second lake. Bays give separation inside one lake: one host, one store, one operator. TKT-01M3FHHBC lists "multi-tenant lakes" as out of scope, and this epic reopens part of that.

### Status

An idea, being fleshed out by a grilling session with the owner. The design decisions and open questions are recorded below as they are settled. No children have been filed yet.

## Notes

**agent:claude-code/7859b064** at 2026-09-29T01:24:39Z

Grilling round 1 with human:sothr, 2026-09-29. Decisions:

- Purpose: a bay is an access-control boundary (a dashboard user or MCP client can be granted some bays and not others), built so that filtering and scoping search, export and views by bay come for free. Rejected: bays as organization only, which is a saved filter and leaves the whole-lake viewer as the only access level. Rejected for now: full isolation (separate retention, backup, encryption per bay), which is close to running separate lakes, and TKT-01M3FHHBC already provides that.
- Unit of routing: the session. Each session is routed by rules on the fields the allowlist already uses (cwd, git remote, harness). A project-wide or device-wide default is expressed as a rule. Accepted cost: a project whose remote or cwd changes can end up split across bays.
- Default bay: every lake has one. Existing data migrates into it, and a session no rule matches lands in it. It is the landing bucket, an inbox. It cannot be deleted. It can be given an alias, a second name it is shown and routable under, which does not make it a different bay. An operator can turn the default off, so every session must match a rule; what happens to an unmatched session then is still open.
- Treat the default like an inbox and aim for zero: once someone starts sorting, the tooling and docs should help empty it (see which sessions are still unsorted and why, and move them in bulk).
