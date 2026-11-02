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
updated_at: 2026-11-02T23:57:01Z
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

**agent:codex/t3-6387cc8a** at 2026-11-02T23:44:08Z

### PR and final local gates

Implementation PR: https://git.local.sothr.com/terva-sh/lampi/pulls/253 (Organize sessions across bays with exact plans and advisory doctor), integrated code head 2b61867cb5feb7418b259be3141629edad538c3d. Final go test -trimpath=false ./... passed. Race tests for catalog, web, CLI, profiledoctor and baydoctor passed on the integrated head; the full race suite had also passed before main integration. go vet ./..., gofmt checks, isolated build and git diff --check passed. git ticket check reports zero errors and only the two pre-existing unrelated long-title warnings. All intended source/ticket changes are committed; no live configuration, credentials or memberships changed.

Request the repository terva-review workflow for the final pushed head and monitor PR checks/review. No merge or live deployment is claimed. The epic and live Vuoro exercise remain blocked solely on the recorded production access/deployment prerequisite; nine software/design/documentation children are done and the explicit-provenance follow-up remains draft.

**agent:codex/t3-6387cc8a** at 2026-11-02T23:48:16Z

Review 3012 on 7ad15f673019ada95df7faca703040a29b224326 found that unrestricted events:read previews exposed grant principals by conflating data reach with administrator authority. Accepted and fixed in this follow-up; rationale, caller audit and regression coverage are recorded under TKT-01M6F5K49E9JCCE6C80FTTNRP3 (MCP: preview session selections and bay migration plans). Fresh model review is required after this substantive fix; earlier implementation-complete notes do not mean the review gate passed.

The live dogfood prerequisite is unchanged: the installed CLI is still v0.11.1 (527d5c368339), and the newly built query bays again refuses before any request because no read token is configured. No lampi connector is currently exposed to this agent. Existing service-account restrictions and the required backed-up deployment remain unresolved; no complete live inventory or split is claimed. Continue through PR review while keeping the live criteria unchecked.

**agent:codex/t3-6387cc8a** at 2026-11-02T23:50:01Z

### Authorization fix validation and review follow-up

Fix head 010aa85611140863b39cbc87e22f5c5b6bf76693 is pushed to PR 253. Full normal tests, catalog/web/CLI race suites, vet, isolated build, whitespace checks and ticket validation passed. Ticket validation retains only the same two unrelated existing long-title warnings. No live state was changed.

Review 3012 finding-1 is accepted and recorded in the maintained disposition store: https://git.local.sothr.com/terva-sh/lampi/pulls/253#issuecomment-37715. The command explicitly identifies this agent's technical assessment and does not claim human merge approval. Fresh full review is running at https://git.local.sothr.com/terva-sh/lampi/actions/runs/2326 (database run 30388), request organization-policy-010aa85. The CLI reported an unexpected-end-of-JSON error on the successful 204 dispatch response; authoritative run inspection confirmed delivery, so it was not blindly retried.

This subsequent commit changes only ticket records. Request carry after the full review through the repository's serialized review workflow, then watch PR 253 for CI, the current-head verdict and new feedback. Do not claim a passing review before its published commit status. Live Vuoro work remains blocked on the separately recorded deployment and read/operator access prerequisites.

**agent:codex/t3-6387cc8a** at 2026-11-02T23:50:53Z

### Review delivery recovery

The previous goal turn made implementation progress: explicit-authority policy fix 010aa85 plus passing normal/race/vet/build gates and accepted finding disposition. Current follow-up inspection found that run 2326 completed its model work but refused publication as superseded: the ticket-only push to 7333af4 arrived before publication. Run 2329 could not carry, because the latest published source review still preceded the code fix (carry_path_not_allowed for docs/bay-migrations.md). Neither failed run is a fresh published review or an accepted gate.

Read both failing job logs using the mandated forgejo-job-log script before deciding the next action. Request a new full review after this ticket-only record is committed and pushed, and keep that head stable until publication. Do not retry carry against the older review. CI for the previous head was still running, and the working tree was clean before this note. Live deployment/read access remain unavailable; no lake changes or live completion are claimed.

**agent:codex/t3-6387cc8a** at 2026-11-02T23:57:01Z

### Review 3013 and hidden-membership fix

Published review 3013 at b5fcfa048782604289ac25aa978a4fa5bf7f4271 confirms the original grant-principal issue is resolved, and raises medium finding-1 for hidden membership inference from last-visible-bay status/default fallback. Review: https://git.local.sothr.com/terva-sh/lampi/pulls/253#issuecomment-37718. Accepted and fixed in 14de2a0c3d87f8d8f844a0b15f127f4f09f83f15; rationale and invariance tests are recorded under TKT-01M6F5K49E9JCCE6C80FTTNRP3 (MCP: preview session selections and bay migration plans).

Advisory outcomes now use only visible memberships and holds, omit full state fingerprints, and report needs_admin_review without inferring a last-bay verdict or default fallback. Full administrator previews retain all-state validation, fallback and atomic-apply safety. Catalog artifact comparisons cover default/named source, remove-only, no-op, add and multiple destinations under hidden membership/hold mutations; HTTP/MCP exercise both reported probes with actual scoped read tokens.

Full go test -trimpath=false ./..., catalog/web/CLI race checks, go vet ./..., isolated build, gofmt and git diff --check passed after the fix. Ticket validation has zero errors and only the same two preexisting unrelated title warnings. Record the accepted disposition while the PR is open, push all source/ticket records together, then request a fresh full review at that stable final head. No ticket-only push should supersede a running review again.

Live dogfood remains incomplete: the private read-token request timed out without delivery, as recorded in TKT-01M6F5A01MX2ZCP9J7M3GGD7M0 (Preview and separate Vuoro review sessions into a bay). Deployment and authorized live read/operator access are still required. Do not check off live requirements from synthetic tests or sampled evidence.
