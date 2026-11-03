---
schema: 3
id: TKT-01M6F5A01MX2ZCP9J7M3GGD7M0
title: Preview and separate Vuoro review sessions into a bay
type: spike
status: blocked
status_reason: Live lake inventory requires read-token or service-account access and deployment of the new server tools; neither is available in this session. No live migration was applied.
priority: normal
due_on: null
labels:
  - area/ops
  - area/catalog
  - policy
assignees: []
milestone: null
parent: TKT-01M6F5K38C9N9Y1TRX4PZB7JSW
origin: null
dependencies:
  - TKT-01M6F5K49E9JCCE6C80FTTNRP3
  - TKT-01M6F5K429GX4TZFJB35TV3KC7
  - TKT-01M6F5K4X6SQP6Y1VXQ77JBYE0
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-11-02T22:15:33Z
updated_at: 2026-11-03T18:13:14Z
created_by:
  id: agent:codex/t3-6387cc8a
  name: ""
updated_by:
  id: agent:codex/t3-6387cc8a
  name: ""
extensions: {}
---

## Description

### Owner goal

Use lampi's own agent interfaces to inspect the local lake and separate Vuoro code reviews from project-development sessions. The owner identified the newly ingested reviews as Vuoro running Claude Code. This is session grouping into a bay; creating a second capture profile is not required merely to separate already captured sessions.

### Verified evidence

On 2026-11-02, all six existing MCP recall tools succeeded against the local lake. Search and read_events identify review sessions under /home/sothr/.local/state/vuoro/worktrees/<timestamp-id>. Sample reviewer sessions 01M6F532E20X2S9YFJQP9ZR4TR and 01M6F4Z4BV9SWF5TXE375T7XQ0 begin with a prompt to review local commits. Companion 01M6F55Q2DJCFV57PGW0WFE87W begins with a prompt to check findings and mark them with rate_draft, has no project_id, and shares the first reviewer's working-directory hash. Include both roles in a code-review bay. SDK origin and harness=claude alone also describe other sessions and are not sufficient selectors.

The current agent consumes the default capture profile with 75 allow rules. Its cached profile already permits /home/sothr/.local/state/vuoro/worktrees. Other existing allow rules mention Vuoro paths on macOS, but their review purpose and current captured sessions have not been verified. Do not extrapolate one machine's directory layout to the whole lake.

### Proposed investigation and migration

First obtain permitted metadata access to the current bay list, memberships, routing rules and working-directory evidence. The current events:read MCP surface cannot enumerate these; local lake data is protected by its service account. TKT-01M6F56Z6QP6C2P3M1T7J923BV (MCP and CLI: discover bays, projects and session metadata) records that tooling gap.

Evaluate code-reviews as the target bay name and the verified Vuoro review working-directory prefix plus harness=claude as an initial selector. Check whether this folder holds only review workflows, enumerate other machines/layouts explicitly, and inspect controls before accepting that selector. Confirm intended readers because a bay is also an access boundary.

For an operator with lake-host access, the existing CLI supports creating the target bay, adding an --add rule with --cwd-prefix and --harness claude, and moving existing sessions from default with the same filters and --dry-run first. Substitute only verified selectors. Adding a rule affects subsequent manifests; apply-rules only adds membership to existing sessions, so neither alone removes reviews from default. A move into a bay requires that bay to exist even in dry-run. A session-specific selection cannot currently be passed as a bulk UID list, and purpose matching is not implemented.

Use the existing metadata routing when validated. If review and development sessions share all available match fields, design explicit provenance/classification and a selection preview before attempting that subset. Leave the capture allowlist alone unless an independent capture-policy change is requested. Preserve session identities and artifacts; do not re-register devices or re-import sessions to regroup them.

### Limits and alternatives

No live bays, memberships, grants, routing rules or profiles were changed. No complete move preview has been run. Searching for review or vuoro returned continuing pages and is not an exact inventory. Routing an entire repository or all Claude sessions would also capture development work. A separate capture profile would not reclassify stored sessions. Store only metadata and decisions in this ticket, never token values or raw transcript dumps.

## Acceptance criteria

- [ ] Inventory current bay membership and enumerate candidate review sessions, including finding-rating companions and sessions without project IDs.
- [ ] Validate directory selectors against candidate prompts and representative development sessions, and produce a complete dry-run move list with exclusions.
- [ ] Specify future routing and existing-session moves separately, including required reader grants and behavior on later manifests.
- [x] Record the approved split or the precise tooling still needed, without claiming a sampled search is a complete migration preview.
- [ ] Exercise the new discovery/selection tools and doctor recommendations on the Vuoro case, recording false positives, companion coverage and any remaining UX/tool gaps.
- [ ] Review add versus move and one versus multiple destinations explicitly, verify future manifests do not unexpectedly restore source memberships, and record any operator-approved live changes separately.

## Implementation plan

Verify local configuration and read access without changing the running agent/lake. Exercise new services on a sanitized Vuoro-shaped synthetic lake, separately record any live inventory/reader/routing evidence that is actually available, and leave unsupported production assertions unchecked. Do not deploy, mint credentials or apply a live split without concrete access and reviewed intent.

## Notes

**agent:codex/t3-6387cc8a** at 2026-11-02T22:20:33Z

The owner expanded the planning scope to multi-bay UX, agent/CLI tools and doctor heuristics. This ticket remains the real-world validation exercise under TKT-01M6F5K38C9N9Y1TRX4PZB7JSW (Discover sessions and organize them into one or more bays). Its original proposed single review bay is an initial case, not a one-destination restriction. Validate reviewer/rater cohorts and missing project IDs using TKT-01M6F56Z6QP6C2P3M1T7J923BV (MCP and CLI: discover bays, projects and session metadata); consume the shared plan from TKT-01M6F5K45WCQKE37JN4CC0WEY7 (Bays: plan and apply selected sessions to multiple destinations) and recommendations from TKT-01M6F5K4X6SQP6Y1VXQ77JBYE0 (Doctor: suggest bay destinations with supporting evidence). Do not treat a text-search page or a heuristic suggestion as authorization to migrate the live lake. The new backlog can be implemented and tested on synthetic data before deployment; a deployment prerequisite and live operator decision must be explicit when this exercise is picked up.

**agent:codex/t3-6387cc8a** at 2026-11-02T23:36:36Z

### Current access and deployment evidence

Read-only local inspection on 2026-11-03 UTC verified the configured default lake at loopback, consuming the default capture profile with 75 allow rules. The installed CLI identifies as v0.11.1 (527d5c368339). The running system service uses the terva-lampi account and a service-owned catalog. This session cannot read that catalog; the noninteractive service-account readability check failed because sudo needs a password. The newly built discovery CLI refuses before making a request because no read token is configured. No credential file contents were read or copied. No current complete lake inventory, reader grants, routing catchment or migration plan can therefore be claimed.

The new server-side discovery/selection/doctor/preview tools require deployment of this branch or a subsequent release. A backup and explicit live deployment decision belong before replacing the running service; neither was performed. Required access is an existing appropriately scoped events:read token for agent discovery or authorized host read access, plus administrator/host authority for a full apply preview. Do not substitute the device upload token or create/re-register devices to obtain it.

### Safe validation completed / remaining live work

The synthetic CLI/HTTP/MCP/dashboard fixtures exercise reviewers and unlinked rating companions, development controls, scoped metadata, mapped recommendations, exact exclusions, add versus move to multiple destinations, complete before/after previews and verification. They do not establish a production false-positive rate or approve any live changes. Before applying a production split, enumerate every reachable metadata page or complete bounded exact selector, inspect opening evidence and development controls, verify each machine's layout, review proposed/existing destination readers and hold/refusal reasons, compare add/move, preview future routing separately, and verify later manifests. Until access/deployment is available, the original sampled evidence remains sampled and all live inventory/move claims remain unchecked.

**agent:codex/t3-6387cc8a** at 2026-11-02T23:36:50Z

in-progress to blocked: Live lake inventory requires read-token or service-account access and deployment of the new server tools; neither is available in this session. No live migration was applied.

**agent:codex/t3-6387cc8a** at 2026-11-02T23:56:03Z

A private T3 secret-card request for an existing events:read token timed out without delivery during the review wait. No secret value or credential file was received, read or persisted by this agent. Authorized read access and deployment of the new tools therefore remain outstanding. The request timeout is not permission to create credentials or repurpose the device upload token. No live memberships, routes, grants, capture configuration or service state changed.

**agent:codex/t3-6387cc8a** at 2026-11-03T00:32:16Z

### Tooling is merged; production prerequisites still outstanding

The complete discovery, selection, migration-preview/apply/receipts, dashboard organization and advisory doctor implementation landed in PR 253 (Organize sessions across bays with exact plans and advisory doctor): https://git.local.sothr.com/terva-sh/lampi/pulls/253. Both forges' main branches are synced at merge 476a02c20abbe0f5a42a0f5a9b592aabb6a43f2f. Final PR head efc0c6fb228222602aa059f39138dd85896567a2 passed complete Forgejo CI and a carried clean model review. docs/organizing-sessions.md, docs/bay-doctor.md and docs/bay-migrations.md contain the end-to-end workflow, access boundary and backup/upgrade procedure.

Revalidated after integration: the live installation is still v0.11.1 (527d5c368339); query bays from the built implementation refuses before any request because no read token is configured; sudo -n service-account access requires a password; no lampi connector is exposed. The earlier private token request received no delivery. Do not retry it repeatedly, repurpose device credentials, register/adopt devices or infer that merging deploys the live service.

This ticket remains blocked pending backed-up deployment and authorized read/operator access. Its five live acceptance checks stay unchecked: complete candidate/membership inventory; prompt/control and selector validation with exclusions; reader/future-routing decisions; actual doctor/discovery usage and error assessment; and add/move/multiple-destination plus later-manifest verification. Synthetic fixtures and the clean source review prove tooling behavior, not the production inventory or an approved split. No live service, profile, credentials, grants, rules or memberships were changed.

**agent:codex/t3-6387cc8a** at 2026-11-03T18:13:14Z

### Deployment prerequisite resolved; read access still unavailable

Resumed the full epic TKT-01M6F5K38C9N9Y1TRX4PZB7JSW (Discover sessions and organize them into one or more bays) against current state on 2026-11-03. The immediately previous goal turn was a status restatement, not progress or a verified live wait. This turn changes the prerequisite assessment based on fresh evidence; it does not claim the live exercise is complete.

TKT-01M6FRDB3NK7SJGFQCJ43WGXCT (Deploy v0.12.0 to the dogfooding lake) records the operator-run migration to schema 23, preserved session/membership counts and resumed capture. Fresh inspection confirms /usr/local/bin/terva-lampi reports v0.12.0 (ccb9e4cc5111), and the active lake unit runs that path. Loopback health returns 200 and the read bays/sessions routes return 401 without credentials. Thus deployment of the session discovery/organization tools is no longer outstanding. This supersedes prior notes claiming both deployment and access are unavailable. The recent prefix-hide feature in PR #259 is separate and does not need deployment for this exercise.

The shell's terva-lampi still resolves to v0.11.1 (527d5c368339), the unchanged workstation agent installation. Calling the verified v0.12.0 binary directly reaches its new query bays command, but it refuses before a request because no read token is configured. LAMPI_READ_TOKEN_FILE is unset. sudo -n service-account readability still requires a password. No lampi tools are exposed in this turn's tool catalog. No credential/auth file was read or copied, and no device upload credential was repurposed.

Because the deployment prerequisite had actually changed, requested an existing appropriately scoped events:read token once via a private T3 secret card. The 60-second request timed out with no delivery. No secret value/file was received. Do not retry automatically or treat the timeout as approval. Next independent progress requires authorized read access (existing private read-token file or an exposed authorized lampi connector); full membership/readership review additionally needs operator authority. No service, capture configuration, profile, credential, bay, grant, route or membership changed.

The five remaining live acceptance checks remain unproven: complete candidate/membership inventory; opening-prompt and development-control validation with exclusions; reader grants and future routing; actual doctor/discovery coverage and false positives; and add/move/multiple destinations with later-manifest verification. Source and synthetic coverage are already integrated but cannot prove those live requirements. Keep this exercise and the epic incomplete. Updated the walkthrough to defer changing deployment/access facts to this ticket rather than preserve an obsolete deployment blocker in reference documentation.
