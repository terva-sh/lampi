---
schema: 3
id: TKT-01M6F5A01MX2ZCP9J7M3GGD7M0
title: Preview and separate Vuoro review sessions into a bay
type: spike
status: in-progress
status_reason: null
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
references:
  - ref: artifact:vuoro-proposed-split-20261103
    path: .tickets/artifacts/TKT-01M6F5A01MX2ZCP9J7M3GGD7M0/vuoro-proposed-split.json
  - ref: artifact:vuoro-approved-host-runner
    path: .tickets/artifacts/TKT-01M6F5A01MX2ZCP9J7M3GGD7M0/vuoro_approved_move.py
claim:
  actor: agent:codex/t3-6387cc8a
  branch: dogfood/vuoro-live-preview
  worktree: /home/sothr/.t3/worktrees/lampi/t3-6387cc8a
  commit: a5e6954de885b78e716f6e69909dffd879372037
  session: null
  claimed_at: 2026-11-03T21:16:07Z
  expires_at: null
archive: null
created_at: 2026-11-02T22:15:33Z
updated_at: 2026-11-03T21:55:20Z
created_by:
  id: agent:codex/t3-6387cc8a
  name: ""
updated_by:
  id: agent:codex/t3-6387cc8a
  name: ""
extensions: {}
---

## Description

### Owner goal and authority

Dogfood Lampi's MCP/CLI interfaces on the actual local lake, identify Vuoro reviews running Claude Code, and prepare a reviewed split from development into one or more bays. A capture profile controls uploads; stored bays perform this organization without re-registration or re-import. The owner approved moving exactly the 1,287 frozen UIDs from default into a new code-reviews bay, with no new reader grants, after a fresh full administrator preview. Preserve unrelated memberships and all explicit exclusions. No future routing, profile or service change is authorized. Host execution access remains outstanding; do not ask for the same split approval again.

### Current verified state

The lake runs v0.12.0 and owner-provided events:read access succeeds through the deployed CLI and direct MCP. Complete reachable metadata traversal on 2026-11-03 found 2,319 unique sessions and one visible bay, default. Full administrator membership, reader-grant and installed routing inspection remains unavailable through that read interface. Historical notes below preserve earlier access blockers; they are resolved, not current prerequisites.

All 1,298 broad Vuoro directory/project candidates and controls received a bounded opening-event inspection. The verified Linux/macOS worktree cohort contains 1,287 sessions: 645 reviewers (including one change-scope opening), 640 finding raters and two draft mergers. Of these, 1,247 have no project ID. Fix-editor, development and synthetic controls are excluded. Purpose is based on visible opening templates, not trusted workflow provenance or proof of every later turn; 820 selected openings contain truncated diffs, with role evidence visible before truncation.

### Exact proposal and artifacts

The ticket-linked .tickets/artifacts/TKT-01M6F5A01MX2ZCP9J7M3GGD7M0/vuoro-proposed-split.json records the exact frozen UIDs in three requests (500, 500 and 287), observed purpose counts, explicit exclusions, scope/cutoff, evidence digest and operator decisions. Related directory groups remain together. Each scoped MCP move preview changes every selected row with zero rejections; complete_memberships=false requires a fresh administrator preview. These are separate batches, not one atomic operation. Plan IDs/expiry in the artifact are observations, not persistent apply authority.

The proposal adds a hypothetical code-reviews bay, removes default only, and creates no reader grants. Administrators and unrestricted/fixed-session tokens may retain access; default-only readers may lose it. The owner confirmed no new reader grants; verify current readership and name availability during full administrator review. Add-only retains default. A six-session comparison also exercised two hypothetical destinations, code-reviews and project-history; the latter is not part of the proposed live split.

### Future routing and remaining validation

Future routing is separate from these exact historical UIDs. Candidate predicates are harness=claude plus each verified worktree prefix. No rule is installed. The Linux root predicate exceeds the 500-match preview cap; seven complete dated-glob catchment previews union exactly to all 1,281 inventoried root matches. The macOS worktree prefix previews all six. Neither dated subrules nor current purpose evidence predict all future runs. Current installed rules/manifest requests must be inspected before asserting that later uploads will preserve removal from default.

Doctor was exercised through CLI and MCP: complete exact reports cover the cohort; a broad Linux request correctly supplies no preview handoffs when incomplete. A deliberately broad macOS parent mapping includes the known fix editor; narrowing to worktrees excludes it. The synthetic CLI walkthrough passed apply/verify and later-manifest behavior, but no production move, receipt or post-move upload has been verified. Owner approval is recorded. Remaining work is host execution access, full administrator membership/readership/routing review, any separately approved live apply/routing and subsequent verification. Never use a search page, metadata heuristic, read token or this artifact as write authorization.

## Acceptance criteria

- [ ] Inventory current bay membership and enumerate candidate review sessions, including finding-rating companions and sessions without project IDs.
- [x] Validate directory selectors against candidate prompts and representative development sessions, and produce a complete dry-run move list with exclusions.
- [ ] Specify future routing and existing-session moves separately, including required reader grants and behavior on later manifests.
- [x] Record the approved split or the precise tooling still needed, without claiming a sampled search is a complete migration preview.
- [x] Exercise the new discovery/selection tools and doctor recommendations on the Vuoro case, recording false positives, companion coverage and any remaining UX/tool gaps.
- [ ] Review add versus move and one versus multiple destinations explicitly, verify future manifests do not unexpectedly restore source memberships, and record any operator-approved live changes separately.

## Implementation plan

Use the verified deployed CLI and owner-provided read token for a complete paginated metadata inventory; record scope, cutoff and non-snapshot limits. Inspect bounded opening evidence for every real Vuoro worktree candidate and representative same-repository/harness development, editor and synthetic controls, with per-session generation/head and explicit unknown states. Freeze exact historical batches of at most 500 without splitting related directory groups; preserve all unlinked companions and explicit exclusions. Compare MCP add/move and multiple hypothetical destinations, and run CLI/MCP doctor reports including incomplete inventory and broad-path false-positive controls. Preview future predicates independently; compare complete dated Linux catchment partitions to the original root-prefix inventory without claiming a single oversized server preview passed. Keep exact request/evidence metadata as a ticket-linked artifact, never transcript or credentials. Record source/destination readership limits and current-rule visibility gaps, present a concrete split for owner approval, then require a fresh full administrator preview before any live creation/move/grant/route. Verify later manifests and receipts only after an authorized apply; synthetic lifecycle checks remain separate evidence. Publish substantive exercise/docs/ticket changes through the required PR review and sync workflow.

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

**agent:codex/t3-6387cc8a** at 2026-11-03T18:14:22Z

### Revalidation record reviewed

PR #260 (Record deployed discovery tools and remaining Vuoro access blocker), https://git.local.sothr.com/terva-sh/lampi/pulls/260, has a clean full code review for head 1d892fe66614d1bb833d35b7a8117159f60778c7 against base 9e1ae278b4e590390d93b38fadc391d4bf553a35. Review: https://git.local.sothr.com/terva-sh/lampi/pulls/260#issuecomment-37801 ; run f5ec29b5-3128-44fb-bc41-4aa5ac7d29ea in Actions #2385. No findings or dispositions. This is a documentation review and does not independently establish live completion. Carry the verdict over this ticket-only review record and merge only after final-head CI passes, then sync both forges.

The next resumed goal turn revalidated that no read-token environment configuration or exposed lampi tools are available, query bays still refuses for missing read-token access, and service-account sudo still requires a password. No new secret request was made and no live state changed. The original implementation remains complete, while all five live dogfood checks remain unproven. Do not manufacture further implementation work or repeated documentation PRs to stand in for the missing live inspection; the next substantive exercise step requires authorized access.

**agent:codex/t3-6387cc8a** at 2026-11-03T21:16:24Z

### Authorized read discovery resumed

Deployment and authorized read access are now verified. This supersedes the old blocked status and prior access prerequisite notes: the deployed v0.12.0 CLI and direct MCP read_events both succeed with an owner-provided private events:read token. The private credential file was read; no credential contents are recorded here. Token delivery authorizes inspection and previews only. No live state changed.

Complete reachable metadata pagination at cutoff 2026-11-03T21:10:40.365031Z returned 2,319 unique sessions across 12 pages and one visible bay, default (bay_default), with 2,319 reachable sessions. All rows report ready normalization; no metadata or machine list truncation. There are 1,620 unlinked sessions. This is a complete traversal within the token's scope, not an immutable snapshot or evidence about hidden memberships/grant principals.

Broad Vuoro directory/project matching produced 1,298 candidates/controls, not 1,298 proven reviews. The Linux worktree prefix has 1,281 Claude sessions, including 1,242 unlinked sessions across two provenance machines. The macOS worktree prefix has six Claude sessions, five unlinked; the separate macOS editor prefix has one. The other ten include development and synthetic test worktrees. Opening inspection verifies recent reviewer/rater pairs and the macOS reviewer, rater and draft-merger roles; the editor prompt requests fixing findings and is excluded from the initial review-only proposal. Full bounded opening inspection is in progress. The MCP limiter is 30-call burst then two calls/second; early burst errors are incomplete reads and must be retried with pacing, never counted as exclusions or successful classification.

Next: finish per-session purpose evidence, freeze exact paired batches of at most 500, compare one/multiple destination add/move previews, run doctor with incomplete and bounded cases, and preview independent future routing. Full administrator membership, readership and current routing review remains necessary before any live apply. Do not mark live acceptance checks complete from this initial inventory alone.

**agent:codex/t3-6387cc8a** at 2026-11-03T21:28:53Z

### Complete live inspection and scoped previews

Initial inventory cutoff: 2026-11-03T21:10:40.365031Z. A second complete traversal at 21:25:21.582739Z found the same 2,319 UIDs, with six changed heads outside the proposed review layouts. All 1,287 selected purpose-evidence heads match the second traversal. All rows are ready with no clipped metadata or machine lists. Scope remains reachable sessions and visible memberships only; no hidden grant principals or installed routing inventory were exposed.

Inspected the first eight normalized events for every one of the 1,298 broad candidates, checkpointing successful reads and respecting two calls/second after retrying the initial rate-limit burst. No tool errors or unresolved openings remain inside either real review worktree layout. Linux: 1,281 sessions, comprising 641 reviewers, 639 finding raters and one draft merger; 1,242 unlinked. macOS: six, comprising three review-local/MR openings, one change-scope review opening, one rater and one merger; five unlinked. Combined: 645 reviewers, 640 raters, two mergers; 1,247 unlinked. The 820 clipped openings expose their role template before a large diff, so this proves opening purpose, not full transcript coverage or trusted workflow identity. No transcript text was copied into the repository.

There are 642 related directory groups: Linux has 638 pairs, one four-session group and one reachable singleton; macOS has groups of five and one. No unseen companion is inferred for the singleton. All related groups remain within a batch. Broad exclusions are one fix editor, five synthetic captures and five development/unresolved controls. Two additional Claude development controls, 01M6F4ED0G4QTF54ZMV998YZQF and 01M6F8B0RB33WN5MKX3QCZTVZ9, run in the Renki worktrees referenced by review metadata and open with continued implementation work; both are outside the selectors. The artifact contains 13 explicit exclusions/controls and exact requests for the 1,287 selected UIDs.

Direct MCP list_bays, list_projects, list_sessions, get_session, read_events, select_sessions, recommend_bays, preview_bay_migration and preview_bay_rule all succeeded; tools/list reports 14 shipped tools, correcting the reading guide's count. CLI pagination and recommend-bays succeeded. Exact batches 500/500/287 each return complete selection, complete doctor coverage with one preview handoff per UID, and move previews with every row changed, zero rejected, default visible before and code-reviews visible after. Every preview has complete_memberships=false. Read previews expire after 24 hours and are not apply artifacts.

The macOS comparison explicitly excluded the fix editor and deduplicated a repeated reviewer. Add-one retains default; move-one removes default; move-multiple adds code-reviews and hypothetical project-history. The proposed split uses only code-reviews, creates no grants, and needs owner approval plus current complete administrator review. No extra category is necessary merely to separate review history from the inbox.

Doctor's broad Linux request reports complete=false, inspected=500, 500 incomplete_inventory findings and zero preview handoffs. Exact macOS plus development/editor controls yields six suggestions and three unmatched. Deliberately widening to the macOS Vuoro parent yields seven suggestions, including the known fix editor: a concrete false positive avoided by the worktrees mapping. These observations validate this inspected case, not general classification accuracy or future purpose.

The independent Linux root future-rule preview refuses selection_too_large. Complete dated partitions return 6/398/200/190/128/303/56 sessions for 20261028 through 20261103; their union equals all 1,281 initial root-prefix matches. The macOS worktree prefix returns six. No route is installed. Current rules, requested bays, destination availability and reader grants require operator inspection. The freshly run TestOrganizationWalkthroughCompanionsExclusionsAndLaterManifests passed: unchanged manifests retain the move, while a later matching add rule restores default. This is synthetic lifecycle evidence, not a live post-move assertion.

Filed draft follow-ups TKT-01M6HMDAFCSN0N540295A4Y3FZ (Read bounded opening evidence for exact session cohorts) and TKT-01M6HMDAJG6AM55ZER5AG5Z9D4 (Preview large routing catchments without sampling), attached as children of this exercise. Their design/docs criteria preserve scope, bounds and explicit incomplete outcomes. They were not promoted.

Acceptance audit: AC2 is satisfied for a complete inspected exact list and scoped dry-run; AC5 is satisfied for live discovery/selection/doctor use, companion coverage and observed false-positive/tool gaps. AC1 remains unchecked for full membership inventory, AC3 for approved readership/current future-routing review, and AC6 for live later-manifest/receipt verification. Existing AC4 remains satisfied by the precise tooling/access record; no approved live split is claimed. The epic's last criterion remains unchecked. No live bay, membership, route, grant, profile, credential, service or workstation-agent installation changed.

**agent:codex/t3-6387cc8a** at 2026-11-03T21:30:01Z

### Exercise record submitted for review

Opened PR #261 (Dogfood Vuoro organization with exact inspected session batches), https://git.local.sothr.com/terva-sh/lampi/pulls/261, with the ticket-linked exact proposal, real MCP/CLI evidence, documentation and two unpromoted tooling spikes. CI and the required full model review are pending. Requested the owner's concrete live-split decision through an asynchronous card: move the exact 1,287 sessions from default to code-reviews with no new grants after a fresh full administrator preview, add-only instead, or keep previews only. The question explains that default-only readers can lose reach. No answer has arrived and no live change is authorized by elapsed time or read access. Keep AC1/3/6 and the epic's final criterion unchecked until their remaining evidence is obtained.

**agent:codex/t3-6387cc8a** at 2026-11-03T21:30:56Z

### Clean source review

PR #261 received a clean full code review for head daf82e97a51a5a53ee99cb6e317950c054b0d8aa against base a5e6954de885b78e716f6e69909dffd879372037. Review: https://git.local.sothr.com/terva-sh/lampi/pulls/261#issuecomment-37901 ; run 920c9dcc-0516-4234-b9e2-9b38cf2ded4b in Actions #2391. No findings or dispositions. The reviewer explicitly limits its verdict to the supplied diff, not independent proof of live classifications or tests. Actual tool evidence remains the exercise record and private responses; administrator membership/readership/routing checks remain required. Carry this verdict over this ticket-only record, wait for final-head CI, then merge and sync both forges under the authorized repository workflow. No live apply is authorized by the clean review.

**agent:codex/t3-6387cc8a** at 2026-11-03T21:38:24Z

### Owner approved the exact historical move

The owner answered the concrete proposal card: "Approve move; no new grants". This authorizes moving exactly the 1,287 ticket-artifact UIDs from default into a new code-reviews bay after a fresh full administrator preview, while preserving unrelated memberships and excluding editor/development/synthetic controls. It authorizes no new reader grants or future routing rules. This supersedes the description's earlier statement that no live membership/bay change is authorized; that statement now describes the pre-approval boundary. Do not ask the owner to approve this same operation again.

The noninteractive host-access check still fails because sudo requires a password; the read token remains read-only. Opened a private sudo credential card for the approved preview/apply/verify operation. Credential delivery is not a new approval. While waiting, prepare a constrained service-account CLI runner with exact-UID/request checks, complete administrator plans, backup/receipt checks and no grant/rule writes. After the first batch creates code-reviews, later batches require fresh previews resolving that existing destination because bay creation changes the policy fingerprint. No live mutation has occurred yet.

**agent:codex/t3-6387cc8a** at 2026-11-03T21:46:56Z

### Integrated inspection record and rehearsed approved host runner

PR #261 merged after all three final-head checks passed at e40830060c421528343899e477a1ee40d78355a6. Both forges are synced at 58d4127a5837e6e8f1cc60256809e2be948878fb; its watch is off. The owner approval is now recorded on the follow-up branch dogfood/vuoro-approved-move.

Attached a constrained host runner at the new artifact reference. It accepts only the exact three approved requests, uses the deployed CLI under the documented terva-lampi service account for all mutations, obtains full administrator previews, checks effective before/after edits and every UID, backs up the catalog with an online SQLite backup and quick_check, preserves unselected membership, and checks receipts and unchanged grants/rules. It installs no route or grant. Batch one creates the new bay; later batches freshly resolve that destination after policy-state changes. Input plans stay private; no credential is embedded or read by the runner. Sudo credential delivery, if provided, is used only by sudo outside this script.

Rehearsed all three phases against a separate synthetic schema-23 lake using the actual deployed CLI: 1,287 fixture sessions moved in 500/500/287, all receipts and follow-up verification match, and the development control and zero grants/rules are preserved. The rehearsal substitutes the current unprivileged account for service-account dropping; real service-account access is still verified during the first host preview. A fixture startup race initially stopped before schema 23; the rehearsal now waits for the correct schema, and the host runner refuses any other schema. No production mutation occurred during these checks.

The private sudo card remains pending and sudo -n still requires a password. Approval is not missing; host execution access is. Do not request approval of the same split again or use the read token as write authority. After host access is delivered, run preview first and inspect current grants/rules/requested bays before apply; preserve the exact approved membership edits and separately verify later manifests.

**agent:codex/t3-6387cc8a** at 2026-11-03T21:47:59Z

### Host credential delivery timed out

The single private sudo card timed out without delivery. No sudo credential file/value was received or read. Existing events:read access still works but cannot produce complete administrator previews or apply. Do not retry the card automatically. The owner already approved the exact move; the remaining requirement is host execution access (private sudo delivery when the owner requests a new card, or owner execution of the prepared preview). No live membership, bay, grant, route, profile or service change occurred. The complete runner rehearsal passed on the isolated schema-23 fixture; it is not a production move or post-move upload result.

**agent:codex/t3-6387cc8a** at 2026-11-03T21:49:04Z

### Approved execution preparation under review

Opened PR #262 (Prepare host execution for the approved Vuoro review move), https://git.local.sothr.com/terva-sh/lampi/pulls/262. Request a full model review even though the artifact is under .tickets: it is executable host-operation code, not merely a note suitable for carry. The current runner passed another complete isolated CLI rehearsal after adding the schema-23 guard. Review and CI are pending; actual service-account transition, full production administrator preview and live apply are not yet verified.

The owner-approved move remains authorized without a second approval. Host access remains missing: the single sudo card timed out and sudo -n requires a password. No secret was delivered. Do not retry automatically or substitute the read token. Once the owner supplies host access or runs the preview, inspect all full plans, grants, rules and manifest bay requests before invoking the approved apply phase. New grants and routes remain outside the approved operation.

**agent:codex/t3-6387cc8a** at 2026-11-03T21:55:20Z

### Host runner review and constrained recovery

PR #262 full review 3023 reviewed a5718b1f89a231699b6b43a57406770efb46fe45 against main 58d4127a5837e6e8f1cc60256809e2be948878fb: https://git.local.sothr.com/terva-sh/lampi/pulls/262#issuecomment-37913 . All three findings are accepted: pin the approved proposal in every phase (high), require successful full preview plus checked backup before apply (high), and preserve verifiable partial progress after interruption (medium).

The runner now hardcodes the approved original SHA256 and preserves/verifies the exact original proposal bytes in preview, apply and verify. A completion manifest written last binds all preview plans, inventories and backup; apply rechecks those artifacts and backup integrity. CLI input files are flushed before applying. Each returned receipt is saved immediately. Partial verify reconstructs committed receipts from the catalog using plans persisted before mutation, including when CLI output was lost. It reports moved/remaining counts and never silently retries a partial operation. Automatic resume lost to explicit partial verification because an interruption or concurrent policy change requires inspection before fresh remaining-session plans; no extra move approval is needed for the same frozen scope.

Also check grants/rules after each fresh batch preview, pin the first created destination ID, and require exactly one created bay in the first receipt and none later. A synthetic concurrent grant injected after the second plan stops before batch two: only the first 500 moved. This closes the inter-batch policy gap while retaining the CLI's transaction-level stale-plan checks.

Added adjacent operator instructions and repeatable six-case isolated tests using the deployed CLI: full 1,287 move/follow-up, proposal tampering in every phase, incomplete preview, altered backup, committed batch with lost output and partial verification (500 moved, 787 remaining), and concurrent reader grant. Fixture approval digest and privilege transition are substituted only in the test process; actual host/service-account access and production effects remain unverified. Request a new full model review for this executable change, even though its path is under .tickets. The owner-approved live operation remains pending host access; the expired sudo card is not retried automatically and no production mutation has occurred.
