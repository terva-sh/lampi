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
updated_at: 2026-09-29T02:11:10Z
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

**agent:claude-code/7859b064** at 2026-09-29T01:43:56Z

Grilling round 2 with human:sothr, 2026-09-29. Decisions:

- Default bay access: the default bay (under its own name or an alias) can be read only by operators, admins, and tokens granted it explicitly. Nobody gets it implicitly, because it holds whatever has not been sorted yet. A single-owner lake grants its owner the default bay at migration.
- Routing is shared between agent and lake. The lake publishes a list of bays to the client, and an agent can be configured to deposit a session into one or more bays. The lake then applies its own rules at ingest, and a matching rule overrides the request: for example, a session started in a sensitive location lands in a holding bay first, and an operator then moves it back to the bays it asked for, moves it somewhere else for good, or keeps it out of a requested bay. Deployed agents carry reasonable defaults, and the operator keeps control of what goes where. Rejected: lake-only routing (it cannot place a session with nothing to match on, such as a scratch directory with no remote) and agent-only routing (the lake could not keep sensitive work out of a bay).
- Membership: a session can belong to one or more bays. Membership is a catalog relation and never copies data. A machine can submit to several bays. This replaces the round 1 recommendation of exactly one bay per session.
- Storage: one blob store shared by the whole lake, with dedup across bays. A device can use blobs/check to confirm that a hash it can guess exists somewhere in the lake; that is recorded as a known limit, not fixed.
- Moving sessions: bay membership can be edited after ingest, every change is audited (who, when, why), and the rules can be run again over stored sessions.

**agent:claude-code/7859b064** at 2026-09-29T02:11:10Z

Grilling round 3 with human:sothr, 2026-09-29. Decisions:

- Derived views are not partitioned by bay. Membership lives only in the catalog, and every read path (recall, search, export, dashboard, MCP) joins against it. search.db may carry the bay set per session so FTS queries can filter, refreshed when membership changes, without re-projecting. Access to the lake's filesystem (DuckDB on parquet/, the catalog file) is admin-level access, and the docs say so. This corrects the round 2 suggestion of a bay= parquet partition, which cannot work once a session is in more than one bay.
- Lake rule actions: hold (replace the requested bays with a holding bay until an operator or admin releases it), add (keep the requested bays and add another), and deny (keep the session out of one named bay). Hold takes precedence. The bays the agent requested are always recorded, so a release restores them in one step.
- A requested bay the device may not write to: accept the session, place it in the default bay, and record which request was refused and why. A later version may add a UI for working through these held and refused sessions; that is a follow-up, not part of this epic.
- Default bay turned off and nothing places the session: the lake refuses the manifest with a distinct error code. The agent keeps the session pending and reports it as "no bay" in status and in the dashboard's device view. Accepted cost: a refused session exists only on its machine until a rule or grant is added.
- Roles: admin and operator are different roles. Admin can read everything in the lake. Operator is the lesser power: minting registration codes to allow ingest, and an operator can be limited to uploading into specific bays. A viewer sees only the bays it is granted. Grants are (principal, bay, permission), where a principal is an OIDC group, a device, or later an MCP identity. This replaces the round 2 recommendation of operator as the all-bays role.
- The list of bays the lake publishes to a device contains only the bays that device may write to, because bay names can carry client names.
