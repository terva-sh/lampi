---
schema: 3
id: TKT-01M3N8KHW56GVZXHV0KBNSPEX1
title: "Bays: segment one lake and route sessions to a bay"
type: epic
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/catalog
  - area/agent
  - area/auth
  - policy
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
updated_at: 2026-09-30T06:53:51Z
created_by:
  id: agent:claude-code/7859b064
  name: ""
updated_by:
  id: agent:claude-code/7859b064
  name: ""
extensions: {}
---

## Description

### Outcome

One lake can be split into named **bays**, and each bay is an access boundary. A dashboard user, an MCP client or a device is granted some bays and not others, and every read path shows only the sessions in the caller's bays. Filtering search, export and views by bay comes along with it.

A session belongs to one or more bays. An agent asks for bays, the lake applies its own rules on top, and whatever nothing places lands in the `default` bay. The default is an inbox: it holds sessions nobody has sorted yet, only admins and explicit grants can read it, and the tooling and docs help keep it at zero.

### Why "bay"

A bay is part of a lake and shares its water, so it is not a second lake. "Pond" was the first word for this and was dropped because lampi already means pond. "Partition" was rejected because the repository uses it for parquet partitions, and "space" and "collection" because they already appear in the code and docs. `docs/naming.md` records the choice.

### How this differs from many lakes

TKT-01M3FHHBC (Agent onboarding: registration codes, lake config, many lakes) lets one agent send different sessions to different lakes. That is separation by running another lake, with its own host, store and operator. Bays separate inside one lake. TKT-01M3FHHBC lists "multi-tenant lakes" as out of scope, and this epic reopens that for access within one lake, so the first child amends the policy before code lands.

### Design decisions, with the alternatives

The owner settled these in a grilling session on 2026-09-29. The notes on this ticket hold each round, including where an answer changed an earlier one.

- **Bays are an access boundary, not only a filter.** Rejected: organization only, which is a saved filter and leaves one whole-lake viewer as the only access level. Rejected: full isolation with per-bay retention, backup and encryption, which is close to running separate lakes, and TKT-01M3FHHBC already provides that.
- **The unit is the session, and a session can be in several bays.** Membership is a catalog relation and never copies data. A project-wide or device-wide default is expressed as a rule. Rejected: one bay per session, which rules out sharing one session with two audiences without a copy. Rejected: routing by project or by device, which is less flexible than rules over session fields. Accepted cost: a project whose remote or cwd changes can be split across bays.
- **Storage stays shared.** One blob store with dedup across bays. Derived views (normalized JSONL, parquet, search.db) are not partitioned by bay, because a session in several bays would need several copies and a move would rewrite files. Every read path joins against catalog membership. search.db may carry each session's bay set for FTS filtering, refreshed on a membership change without re-projecting. Known limit, recorded and not fixed: `blobs/check` lets a device confirm that a hash it can guess exists somewhere in the lake. Filesystem access to the lake directory is admin-level access.
- **The default bay is the landing bucket.** Every lake has one. Existing data migrates into it, and a session nothing places lands there. It cannot be deleted. It can be given an alias, a second name it is shown and routable under, which does not make it a different bay. Only admins and principals granted it explicitly can read it, operators included. An admin can turn the default off. Then a session that no rule, request or grant places is refused with a distinct error code, and the agent keeps it pending locally and reports it as "no bay". Rejected: a hidden holding area for those sessions, which would be the default bay under another name. Accepted cost: a refused session exists only on its machine until a rule or grant is added.
- **Routing is shared between agent and lake, and the lake wins.** Each lake's entry in the agent's config gains bay request rules shaped like `projects.allow` (cwd prefix, git remote, harness, naming one or more bays) and a `default_bays` list. A lake profile may suggest both, and local config wins, the precedence profiles already have. The lake then applies its rules at ingest:
  - **hold** replaces the requested bays with a holding bay until an admin releases it;
  - **add** keeps the requested bays and adds another;
  - **deny** keeps the session out of one named bay.

  Hold takes precedence. The bays the agent requested are always recorded, so a release restores them in one step. Rejected: lake-only routing, which cannot place a session with nothing to match on, such as a scratch directory with no remote. Rejected: agent-only routing, where the lake could not keep sensitive work out of a bay. Rejected: a per-repository marker file, because a cloned repository could then send your sessions into a shared bay.
- **A requested bay the device may not write to is recorded, not obeyed.** The refused request and its reason are recorded and place nothing, so a session with only refused requests lands in the default bay, or is refused when the default is off. The owner chose the default bay for this case; the default-off case was settled by the agent from the owner's Q11 answer, after review 1377 on #148 found the two rules in conflict. Rejected: refusing the manifest, which would strand the session on the machine over what is usually a missing grant.
- **Every manifest is routed again, add-only.** A session gains a bay when a later append requests one it may write, and the rules run again on each manifest. Nothing is removed automatically. A hold that matches a session already in other bays flags it for review and does not remove it.
- **Admin and operator are different roles, and bays scope the two below admin.** The admin role already exists (TKT-01M3NM61CZ, Admin role and raw artifact access): admin implies operator, which implies viewer, and only an admin reads raw artifacts and mints `lrt_` read tokens. Bays add scope beneath it. An admin reads every bay, including the default, and creates, renames and deletes bays, rules and grants. An operator mints registration codes and manages devices, can be limited to specific bays, and reads session content only in bays it is granted. A viewer reads only the bays it is granted. A grant is (principal, bay, permission), where a principal is an OIDC group, a device, a read token, or later an MCP identity, and the permission is read or write. Rejected: making operator the all-bays role, because letting a machine ingest is a lesser power than reading every session.
- **Read tokens take a bay scope.** An `lrt_` token is scoped today to the whole lake or to listed sessions. It gains an optional list of bays, and a token with a bay list reads only sessions in those bays at the time of each read. A token minted before bays keeps its scope. The broader token model planned in TKT-01M3KAMD1Z (Lake-backed sessions: read path, token scope, byte-exact reads) should carry bays as one of its scopes.
- **Reuse the audit log and the recent sign-in check.** Membership changes, holds, releases, grant edits and bay lifecycle events are queued to the existing audit outbox (`audit.jsonl`) in the same transaction as the change, as registration and raw reads already are. No separate audit table. A dashboard action that adds access (granting a bay, minting a code or read token with bays, moving a session into a bay, releasing a hold) requires a sign-in in the last 10 minutes, as minting a registration code does.
- **Bay rules reuse the profile rule matcher**: `cwd_prefix`, `cwd_glob`, `git_remote` and `git_remote_prefix`, plus harness. That applies to both the agent's bay requests and the lake's hold, add and deny rules, so the profile preview tooling can show what a bay rule would catch. The matcher's open bug TKT-01M3NQ83 (Allow on a folder project allows everything under it, home dirs too) matters here: a bay rule on a folder must not catch everything beneath it.
- **Registration grants a device its write bays.** The grants sit on the lake's pending registration row, as the profile already does (`internal/catalog/registrations.go`), are capped at the minting operator's scope, and are applied when the code is redeemed. The code itself (`internal/regcode` `Code`) does not change. A device's bays are changed later by editing its grants on the lake.
- **A device sees only the bays it may write to.** Bay names can carry client names. The agent gets `terva-lampi bays`, which lists them per lake, and `terva-lampi bays which [PATH]`, which says which lake and bays a session started at PATH would request, and which rule caused each.
- **Bay lifecycle belongs to admins.** A bay has a stable id. A rename keeps the old name as an alias, so agents that request it keep working. Deleting a bay removes it from every membership, a session left in no bay moves to the default, and no data is deleted.
- **Every read path is scoped by bay:** recall, search, excerpts, transcripts, activity, overview counts, and MCP when it lands. On the lake host, `terva-lampi export` is admin-level and gains `--bay`. The training export stays gated by the `projects` allowlist and also by `--bay`. A test lists every catalog query that returns session data and fails when a new one takes no bay scope. Considered and not chosen: a single choke point, such as a query builder that requires a scope, which is stronger but needs `internal/recall` refactored first.
- **Inbox tooling ships with the epic.** `serve bays inbox` lists unsorted, held and refused sessions with a reason each (no rule matched, request refused, hold rule X). Bulk move by filter and `serve bays apply-rules` both take `--dry-run`. A docs guide covers sorting a lake after upgrade and keeping the inbox at zero. The dashboard shows inbox counts and the list with reasons, and lets an admin move one session and release a hold. Those are the dashboard's first writes to session membership, so the web path gets CSRF protection and audit entries. Bulk move and rule editing stay CLI-only.

### Upgrade and compatibility

The upgrade changes no behavior. Existing data is in `default`. Existing viewer and operator groups get an explicit read grant on `default`, so they read what they read before. Existing operators are scoped to all bays for minting. Existing admins read everything, as they already do. Existing devices get a write grant on `default`, and existing read tokens keep their scope. No group is promoted to admin: the owner confirmed that on 2026-09-29 for TKT-01M3NM61CZ, and it supersedes round 4 of this epic's grilling, which proposed making existing operators admins. A lake with no admin group can still manage bays from the host CLI, and startup already warns about the missing group. Startup logs which groups were granted what.

The protocol change is additive, so `capture_protocol` stays 1. The manifest gains an optional `bays` field, and the lake publishes each device's writable bays. An old agent sends no bays and is placed by the lake's rules and the default. The "no bay" error code goes only to agents that announce bay support; an old agent gets a plain 4xx it already backs off on. The lake upgrades before any agent.

### Children, in order

1. Policy and docs: amend `docs/policy.md` and `docs/architecture.md`, and add bay to `docs/naming.md`. Owner sign-off.
2. Catalog: bays, membership, requested bays, grants; audit through the existing outbox; schema bump and migration.
3. Bay scope for operators, viewers and read tokens; bay grants on registration and devices. The admin role itself already exists.
4. Read-path scoping and the query-list test.
5. Protocol and lake routing.
6. Agent bay requests and `terva-lampi bays`.
7. Inbox tooling and the guide.
8. Dashboard: bay scoping, inbox view, move and release.

Child 4 lands before child 5, so no bay can hold data until every read path honors bays.

### Left to the children

Bay name syntax; `serve backup`, `fsck` and `purge` coverage of the new tables; how long audit entries are kept. MCP identity grants wait on TKT-01M3FPWCH (MCP: authenticate clients as OIDC users).

### Related work

- TKT-01M3N22HC (Per-device overrides on top of agent profiles): bay requests are a natural field for the device override layer once both exist.
- TKT-01M3FPP3 (Session recall: one query surface for the web UI and an MCP server): the read-path child scopes that query layer.

### Out of scope, filed as follow-up drafts

A dashboard triage flow for held and refused sessions, and per-bay retention and purge ("delete a bay and its data").

## Acceptance criteria

- [x] A viewer granted one bay sees only that bay's sessions in the dashboard, search, excerpts and recall, and a test fails when a query returning session data takes no bay scope
- [x] An agent requesting bays A and B lands one session in both with one stored copy, and a lake hold rule sends a matching session to the holding bay with its requested bays recorded, and a release restores them
- [x] A request for a bay the device may not write lands the session in default with the refused request recorded
- [x] With the default bay off, an unplaced session is refused, stays pending on the agent, and is reported as no bay
- [x] A scoped operator can mint a code only for bays within its scope, and cannot read sessions in a bay it is not granted
- [x] serve bays inbox lists every unsorted, held and refused session with its reason, and bulk move and apply-rules have --dry-run
- [x] An existing lake upgrades with no change in behavior: all data in default, viewers and operators still read it, no group becomes admin, and old agents keep syncing
- [x] A read token scoped to a bay reads raw artifacts only of sessions in that bay

## Definition of done

- [x] All children of this epic are done
- [x] docs/policy.md records bays and the reopened multi-tenant decision with owner sign-off
- [x] The triage UI and per-bay retention follow-ups are filed as drafts

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

**agent:claude-code/7859b064** at 2026-09-29T14:52:08Z

Plan revised against main at 5ab86ad on 2026-09-29, at the owner's request, for work that landed while this epic was being grilled.

- The admin role exists (TKT-01M3NM61CZ, Admin role and raw artifact access; #132, #134, #135). Admin implies operator implies viewer. The roles child no longer builds the split and is retitled "Bays: bay scope for operators, viewers and read tokens".
- Upgrade: the owner confirmed on TKT-01M3NM61CZ that no group is promoted to admin on upgrade. That supersedes round 4 here (existing operators become admins). Existing viewers and operators get read on default instead, so behavior is still unchanged. Epic AC 6 and the scope child's AC were rewritten to match.
- Raw-read tokens (lrt_) are the "tokens granted explicit access" from round 2. They gain an optional bay list; tokens minted before bays keep their scope.
- Audit goes through the existing audit outbox (audit.jsonl), not a new table, and dashboard actions that add access require a sign-in in the last 10 minutes, as minting does.
- Bay rules reuse the profile rule matcher, which gained cwd_glob and git_remote_prefix, and inherit the open folder bug TKT-01M3NQ83.

**agent:claude-code/7859b064** at 2026-09-29T20:04:29Z

Implementation landed. Each child merged as its own Forgejo PR, each after green CI, a terva-review with a disposition posted for every finding, and a check that it contained current main:

- #148 docs (TKT-01M3NNF1YN): blocked only on owner sign-off
- #150 catalog (TKT-01M3NNF21H)
- #152, #154 scope for operators, viewers and read tokens (TKT-01M3NNF24A)
- #156 read scope (TKT-01M3NNF27A)
- #157, #159 manifest bays and lake routing (TKT-01M3NNF29W)
- #160, #161 agent bay requests (TKT-01M3NNF2CE)
- #162 inbox tooling and sorting guide (TKT-01M3NNF2FE)
- #163 dashboard (TKT-01M3NNF2K3)

Each child ticket's notes carry that child's review findings and the alternatives that lost. The decisions that cross children:

### Reads and scope
- A zero Scope reads nothing. Every exported Catalog method takes a Scope, or is listed with its reason in scope_test.go.
- A route test asks every web GET for a session outside the viewer's bays. With scoping switched off it found 59 leaks across 14 routes.
- Main's conflict actions (#153, #155) go through the read guard, so an operator who is not an admin gets 404 outside their bays.
- A request with no device is the tokenless lake (every bay) only when the lake has no tokens. This one rule, requestDevice, backs deviceScope, hello and manifests.

### Holds
A hold keeps a session for review, and nothing else places it:
- every request waits while held;
- a move (bulk or single) refuses a held session;
- DeleteBay refuses a bay with active holds;
- release matches rules against the harness and project of the post that last routed the session, kept on the hold row.

### Deny
A deny is decided before a request's outcome is recorded, both at ingest and at release. A denied ref is refused and listed in refused_bays; the ack never says why.

### Sessions in no bay
No write leaves a session in no bay, and every path that could sends it to the default instead. A move from the default is how an admin places one found by fsck (CLI and dashboard). fsck reports memberships, grants, rules, aliases and holds that name a bay that is gone.

### Process
- A PR whose branch lacked only the lower PR's own merge commit was merged without merging main again. This was done when `git merge-tree` of main and the branch equalled the reviewed tree (#156, #159).
- Every other time main moved, main was merged in and the PR was reviewed again.
- One finding was rejected: review 1437, a claim of no release path, which ships in the next PR.
- A second was rejected: review 1451, which assumed queued manifests are reposted as stored; they are prepared again each pass.

### Still open
- TKT-01M3NNF1YN needs the owner to read the Bays section of docs/policy.md and sign off. The epic stays open until then.
- Two follow-up drafts are filed and not started: TKT-01M3NNF2NN (dashboard triage flow for held and refused sessions, which also covers editing device grants) and TKT-01M3NNF2R9 (per-bay retention and purge).

**agent:claude-code/7859b064** at 2026-09-30T06:53:51Z

Closing: which tests back each acceptance criterion.

1. Viewer scope: TestNoRouteShowsASessionOutsideTheViewersBays (web) and TestEverySessionReadTakesAScope (catalog, which fails on an unscoped query). Recall search and events take a Scope. MCP is not built yet; it will take the same Scope when it is.
2. Two bays, one copy: TestBayRequestsRouteASessionToTwoBays. Hold and release: TestAHoldHoldsANewSessionAndFlagsAStoredOne and TestAHeldRequestWaitsForAGrant.
3. Refused request lands in default: TestARefusedRequestLandsInDefault.
4. Default off: TestDefaultOffRefusesOnlyASessionNothingPlaces and TestANoBayRefusalWaitsAndShowsInStatus.
5. Scoped operator: mints are checked against the minter's write grants inside the transaction (ErrBayScope, #154), and read scope comes from TKT-01M3NNF27A.
6. Inbox and dry runs: the TKT-01M3NNF2FE tests.
7. Upgrade: the catalog child's migration (all sessions in default) and the one-time upgrade grants for web groups; capture_protocol stays 1, so old agents sync unchanged.
8. Bay-scoped read token: TestReadTokenBayScope.

## Summary

Bays shipped in #148 and #150–#163, with ticket closures in #164 and this change. One lake is split into bays: every read is scoped to the caller's bays; manifests request bays and lake rules route them (hold, add, deny); the agent asks per lake; admins sort the inbox from the CLI or the dashboard. The owner signed off on the policy on 2026-09-30. Follow-ups filed as drafts: TKT-01M3NNF2NN (triage UI) and TKT-01M3NNF2R9 (per-bay retention).
