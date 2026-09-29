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
updated_at: 2026-09-29T04:01:26Z
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

**agent:claude-code/7859b064** at 2026-09-29T02:40:02Z

Grilling round 4 with human:sothr, 2026-09-29. Decisions:

- Reading the default bay: only admins and principals granted it explicitly. An operator can read it only with an explicit grant, like anyone else. This supersedes the round 2 wording "operators, admins, and tokens". An operator without a grant sees devices, codes, and per-device counts and statuses, not transcripts.
- Registration grants the new device its write bays, capped at the minting operator's own scope. Fact checked at 600438c: the code itself (internal/regcode Code) carries only url, lake id, key, secret and expiry, and the profile lives on the lake's pending registrations row (internal/catalog/registrations.go) and is applied when the code is redeemed. Bay grants follow the same path. Nothing in the code has to be updated, and changing a device's bays later is an edit to its grants on the lake.
- Bay lifecycle: only an admin creates, renames or deletes a bay and changes rules and grants. A bay has a stable id; a rename keeps the old name as an alias. Deleting a bay removes it from every membership, a session left in no bay moves to the default bay, and no data is deleted. Per-bay retention and purge is a follow-up, not part of this epic.
- Upgrade: behavior does not change. Existing viewer groups get an explicit read grant on the default bay. Existing operator groups become operators scoped to all bays and also admins. Existing devices get a write grant on the default bay. The web config gains an admin role mapping, and startup logs which groups were granted what.

**agent:claude-code/7859b064** at 2026-09-29T03:56:42Z

Grilling round 5 with human:sothr, 2026-09-29. Decisions:

- Agent bay requests (the owner left this to the agent, putting developer and user experience first). Each lake's entry in config.json gains bay request rules shaped like projects.allow (cwd prefix, git remote, harness, naming one or more bays) and a default_bays list. A lake profile may suggest both, and local config wins, the precedence profiles already have. The lake's hold/add/deny rules are the backstop for operator control. For the developer's side: `terva-lampi bays` lists the bays each lake lets this device write to, and `terva-lampi bays which [PATH]` says which lake and bays a session started at PATH would ask for and why. A request for a bay the device cannot write to warns in status and is still sent, so the lake records it. Rejected: a per-repository marker file, because a cloned repository could then direct your sessions into a shared bay.
- Protocol: additive, capture_protocol stays 1. The manifest gains an optional bays field, and the lake publishes each device's writable bays. An old agent sends no bays and is placed by the lake rules and the default bay. The new "no bay" error code goes only to agents that announce bay support; an old agent gets a plain 4xx it already backs off on.
- Growing sessions: every manifest is routed again, add-only. Newly requested bays that are allowed are added. Rules run again. Nothing is ever removed automatically: a hold that matches a session already in other bays flags it for review and does not remove it.
- Inbox tooling in this epic: `serve bays inbox` with a reason per session (no rule matched, request refused, hold rule X), bulk move by filter with --dry-run, `serve bays apply-rules` with --dry-run, and a docs guide to sorting a lake after upgrading and keeping the inbox at zero. The dashboard is part of this epic; how much of it is being settled in round 6. A full triage UI for held and refused sessions is a follow-up.

**agent:claude-code/7859b064** at 2026-09-29T04:01:26Z

Grilling round 6 with human:sothr, 2026-09-29. Decisions:

- Dashboard scope in this epic: inbox counts and a list of unsorted, held and refused sessions with their reasons, plus admin actions to move one session between bays and release a hold. These are the dashboard's first writes to session membership, so the web path needs CSRF protection and records audit entries. Bulk move and rule editing stay CLI-only with --dry-run. The full triage flow is a follow-up.
- Read paths: every path that returns session data filters by the caller's read bays: recall, search, excerpts, transcripts, activity, overview counts, and MCP when it lands. On the lake host, `terva-lampi export` is admin-level and gains --bay; the training export stays gated by the projects allowlist and also by --bay. A test lists every catalog query that returns session data and fails when a new one takes no bay scope. Considered and not chosen: a single choke point (a query builder that requires a scope), which is stronger but needs internal/recall refactored first.
- Children, filed as drafts in this order: (1) policy and docs amendment, owner sign-off; (2) catalog: bays, membership, requested bays, audit, grants, migration; (3) roles: admin split from operator, operator bay scope, grants on registration and devices; (4) read-path scoping and the query-list test; (5) protocol and lake routing; (6) agent bay requests and `terva-lampi bays` / `bays which`; (7) inbox tooling and guide; (8) dashboard. 4 lands before 5, so no bay holds data until the read paths honor bays. Follow-up drafts outside the epic: the triage UI, and per-bay retention and purge.
