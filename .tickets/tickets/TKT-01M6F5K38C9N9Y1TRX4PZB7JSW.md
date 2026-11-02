---
schema: 3
id: TKT-01M6F5K38C9N9Y1TRX4PZB7JSW
title: Discover sessions and organize them into one or more bays
type: epic
status: blocked
status_reason: Implementation is complete; live Vuoro dogfood remains blocked on deployment and authorized read/operator access. See TKT-01M6F5A01MX2ZCP9J7M3GGD7M0.
priority: normal
due_on: null
labels:
  - area/server
  - area/catalog
  - area/search
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
created_at: 2026-11-02T22:20:31Z
updated_at: 2026-11-02T23:42:23Z
created_by:
  id: agent:codex/t3-6387cc8a
  name: ""
updated_by:
  id: agent:codex/t3-6387cc8a
  name: ""
extensions: {}
---

## Description

The owner wants to dogfood lampi through MCP and CLI, separate Vuoro code-review sessions from development, and make organizing sessions into one or more bays a usable routine. On 2026-11-02 the owner explicitly requested tickets for missing tools and documentation, migration UX based on actual usage patterns, and doctor recommendations driven by determined heuristics. This epic records that work; it does not authorize a live migration or promote its draft children.

### User outcomes

A person or agent can discover the lake, identify a reproducible set of sessions, see why each is selected, choose one or more destinations, inspect exact before/after membership and readership implications, and apply the reviewed operation with suitable operator authority. People can sort stored sessions once, establish future routing separately, and verify what subsequent uploads do. Doctor can suggest destinations with evidence and uncertainty, handing the user into the same preview flow.

### Observed usage and design boundaries

The first real case is Vuoro on Claude Code: reviewer sessions and finding-rating companions share timestamped review worktrees; companions may have no project ID. Project labels may be paths or remotes. A broad harness, repository, SDK origin or the word review does not identify the intended subset. Verify other layouts and roles rather than assuming every Vuoro worktree is a code review.

Bays support multiple membership and are access boundaries. Adding membership retains other bays; moving explicitly removes selected source memberships. The default is an inbox, not the development category. A bay named for review content is distinct from a quarantine/hold bay. Capture profiles, agent bay requests, lake routing and stored membership are separate controls. Preserve identities and artifacts when regrouping.

### Scope and sequencing

Children cover usage/interaction design, authorized metadata discovery, bay filters for recall, reproducible selection and migration plans, read-only MCP previews, the existing dashboard triage backlog, heuristic validation, doctor surfaces, documentation and a Vuoro dogfood exercise. Follow dependency edges. Discovery and the usage spike are the first independent slices. Each feature updates its reference documentation; the documentation child supplies the complete cross-surface walkthrough.

Reuse existing query, scope, routing, audit and profile-doctor code. Keep membership mutation under explicit operator authority; events:read and a heuristic suggestion confer no write permission. Synthetic or sanitized fixtures replace copied production transcripts. Source observations live in the discovery and Vuoro tickets. Retention/purge and machine-install diagnostics remain separate existing work.

## Acceptance criteria

- [x] MCP and CLI can discover reachable sessions and select bays consistently across recall tools.
- [x] CLI and dashboard support reviewed additions and moves into one or more bays using reproducible selections and exact membership previews.
- [x] Doctor suggests evidence-backed bay destinations, can abstain, and hands recommendations to the same preview without applying them automatically.
- [ ] Documentation and the Vuoro exercise verify reviewer companions, mixed sessions, multi-bay membership and future routing behavior.

## Notes

**agent:codex/t3-6387cc8a** at 2026-11-02T22:20:37Z

### Work breakdown

- TKT-01M6F5K3BTS8X9BTFZTECWYDCR (Spike: session identification and multi-bay migration UX)
- TKT-01M6F56Z6QP6C2P3M1T7J923BV (MCP and CLI: discover bays, projects and session metadata)
- TKT-01M6F5K429GX4TZFJB35TV3KC7 (MCP and CLI: filter recall and analysis by selected bays)
- TKT-01M6F5K45WCQKE37JN4CC0WEY7 (Bays: plan and apply selected sessions to multiple destinations)
- TKT-01M6F5K49E9JCCE6C80FTTNRP3 (MCP: preview session selections and bay migration plans)
- TKT-01M3NNF2NNAETK0YG9WVBQW21G (Bays: guided selection, multi-bay migration and inbox triage)
- TKT-01M6F5K4SJPKTM2867HZEHSWR0 (Spike: validate heuristics for bay recommendations)
- TKT-01M6F5K4X6SQP6Y1VXQ77JBYE0 (Doctor: suggest bay destinations with supporting evidence)
- TKT-01M6F5K514R3K4KSCPE766K25F (Docs: discover, classify and migrate sessions across bays)
- TKT-01M6F5A01MX2ZCP9J7M3GGD7M0 (Preview and separate Vuoro review sessions into a bay)

The dashboard item reuses and expands the existing held/refused triage ticket, preserving its completed dependency. The two earlier dogfood drafts are reused. No duplicate machine-setup doctor or retention/purge work is created. All tickets remain draft, unchecked and unclaimed. Dependency links describe implementation order rather than promoting any item.

**agent:codex/t3-6387cc8a** at 2026-11-02T22:57:45Z

The owner explicitly promoted the epic and all ten children on 2026-11-02 and requested autonomous completion. This supersedes the earlier draft-only planning status. Start with the usage contract and metadata discovery, then follow dependency order through filters, migration services, MCP/dashboard, heuristics/doctor, documentation and dogfood. Commit small reviewable slices and keep decisions and checks in the relevant tickets. The initial working tree was clean. Do not mark live verification complete without actual access and evidence. Repository review/CI workflow applies; no new release tag or credential changes are inferred.

**agent:codex/t3-6387cc8a** at 2026-11-02T23:37:50Z

### Implementation and verification outcome

Nine implementation/design/documentation children are complete on this branch: authorized metadata discovery, consistent bay filtering, exact session selection, atomic multi-bay plans/apply/receipts/recovery, read-only MCP previews, guided dashboard organization/triage, heuristic evaluation, shared advisory doctor, and the end-to-end guide. Full go test -trimpath=false ./... and go vet ./... passed; gofmt is clean and the isolated CLI build succeeds. The explicit trimpath=false test flag overrides this machine's persistent -trimpath setting without editing it; fixture-path failures under the default are environmental.

The synthetic end-to-end case includes reviewer and unlinked rating companion plus same-repository development controls; exact exclusions, add/move into overlapping bays, receipt verification and subsequent manifests are exercised. Reingesting unchanged manifests preserves the move; an explicit later add rule targeting the removed source restores default membership, as documented. Doctor remains uncertain mapped metadata rather than a purpose classifier, and incomplete/alternative evidence cannot bypass preview.

TKT-01M6F5A01MX2ZCP9J7M3GGD7M0 (Preview and separate Vuoro review sessions into a bay) remains blocked on a live read token/service-account access and deployment of the new server tools. No complete production inventory, approved split or live changes are claimed. Its precise prerequisites and observed access failures are recorded there. AC4 remains unchecked because synthetic success is not live dogfood completion. TKT-01M6F9QAGWW2PCF4ESFVVNN643 (Capture explicit workflow and companion provenance for session routing) is a separate draft follow-up for source-backed purpose evidence.

**agent:codex/t3-6387cc8a** at 2026-11-02T23:37:50Z

in-progress to blocked: Implementation is complete; live Vuoro dogfood remains blocked on deployment and authorized read/operator access. See TKT-01M6F5A01MX2ZCP9J7M3GGD7M0.

**agent:codex/t3-6387cc8a** at 2026-11-02T23:42:23Z

Integrated origin/main through 0bd109d, preserving the newer wide tables, named machines/timestamps, grouped profile Health panel and read-token choices. The newly landed project-column migration occupies schema 22, so durable bay migration receipts now append schema 23. Backward-migration fixtures remove both additions when constructing older catalogs; the project-column upgrade test verifies the ordered 21→22→23 steps. Focused catalog/dashboard/CLI integration tests pass after the merge. The pre-merge full race suite passed, and final full normal plus affected-package race/vet/build checks are running on the integrated head before PR handoff. No live deployment or migration has been attempted.
