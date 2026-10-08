---
schema: 3
id: TKT-01M4ERWGJ894HBPWJWSC3KSYR3
title: Deploy dashboard pagination and maintenance to dogfooding
type: task
status: in-progress
status_reason: The owner started the prepared root operator script; backup/install verification is now underway.
priority: normal
due_on: null
labels:
  - area/ops
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:codex/t3code-8afe4a1e
  branch: ops/dogfood-dashboard-92c2dbe
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-8afe4a1e
  commit: d5f9b5274d8cb9a3e09292ddb4252c87765a61bc
  session: null
  claimed_at: 2026-10-08T22:17:25Z
  expires_at: null
archive: null
created_at: 2026-10-08T22:07:04Z
updated_at: 2026-10-08T22:17:25Z
created_by:
  id: agent:codex/t3code-8afe4a1e
  name: ""
updated_by:
  id: agent:codex/t3code-8afe4a1e
  name: ""
extensions: {}
---

## Description

The owner requested deployment of the merged dashboard pagination and Operations maintenance controls to the dogfooding site on 2026-10-08. This request authorizes the site deployment; it does not publish v0.9.0 or upgrade the workstation capture agent.

Deploy main commit 92c2dbec1e9f04d0bdfc14f78213b8a1124749fc (PR #204), stamped as v0.9.0-dogfood.92c2dbe. It is synchronized on both forges, with a clean model review and green full race/vet/format/build and two-platform image gates. No catalog or normalization migration is introduced (schema remains 21).

The previous deployment ticket, TKT-01M44KG2PM151XTBC5RNFTYWC9 — Deploy v0.8.0 to the internal lake and the workstation agent, records the existing installation and root-run checkpoint procedure. Preserve the live configuration, identity, data, and capture-agent binary. Host coordinates and deployment bundle details stay in the external operator handoff. Take a verified stopped-lake backup, keep the previous binary for rollback, install atomically, and verify schema/counts/identity, health, authentication guards, public reachability, and resumed capture.

The current session cannot obtain noninteractive sudo: a password is required. Prepare and validate the complete checksummed operator bundle before requesting the owner's root execution; do not ask for credentials.

## Acceptance criteria

- [x] A checksummed deployment binary and operator script are ready with rollback and verified backup gates.
- [ ] The dogfooding lake runs the merged build with schema, identity, counts, health, and authentication guards preserved.
- [ ] The public site responds and capture resumes successfully; the deployment outcome is recorded.

## Implementation plan

Build the synchronized and gated main commit as a clearly labeled dogfooding binary without publishing a release tag. Adapt the proven root-run deployment script with strict installed-binary/unit/role/schema checks, a verified stopped-lake checkpoint, atomic binary install, immediate rollback on a failed install check, and restoration of capture state. Rehearse backup/fsck and old-to-new startup on isolated synthetic data, and checksum the complete external operator bundle. Ask the owner to run only the prepared root command because noninteractive sudo is unavailable. Verify running version, loopback/public health and auth guards, schema/counts/identity checks from the operator report, resumed capture, and the added UI when an authenticated browser is available. Record the result in this ticket and leave publication of v0.9.0 separate.

## Notes

**agent:codex/t3code-8afe4a1e** at 2026-10-08T22:07:27Z

draft to ready: Owner explicitly requested this dogfooding deployment on 2026-10-08.

**agent:codex/t3code-8afe4a1e** at 2026-10-08T22:12:10Z

Prepared the complete checksummed operator bundle deploy-dashboard-92c2dbe-eWYs3A4N under the external agent-handoffs/lampi directory; README.md contains the root command and rollback steps. It is attached to this ticket. The native binary reports v0.9.0-dogfood.92c2dbe (92c2dbec1e9f), with source outside .tickets identical to merged main 92c2dbe. Shell syntax and all bundle checksums pass; installed unit/drop-in still match the prior successful deployment and live v0.8.0 loopback/public health checks pass.

A rehearsal with synthetic data used the installed v0.8.0 binary to seed one session, verified a stopped-lake backup and fsck, upgraded to the dogfooding binary with schema 21 and unchanged counts/integrity, re-posted the same manifest without growth, and restarted the old binary successfully. No live tokens/configuration were used. The script preserves capture configuration/state, checksums the deployment assets, installs atomically, verifies the newly mounted maintenance route denies anonymous requests, and attempts an automatic binary rollback on an install failure. Full stored-blob compaction is not triggered.

Deploying the merged source as an explicitly unpublished dogfooding build was chosen over publishing v0.9.0 because the owner authorized the site deployment and has only asked to consider a public release. The stable release ticket remains draft; the capture-agent binary is unchanged. release.Parse deliberately rejects prerelease stamps, so agent self-update will not select this dogfooding binary as a stable release.

Root execution is pending: sudo -n true returned a password requirement. The prepared command was handed to the owner through an async request. A completed script writes a credential-free DEPLOYMENT_RESULT into the bundle so this session can verify the result. The native shared browser reaches the sign-in page and requires the owner's login for an authenticated UI check. No live service was stopped or changed by this session before that root step.

**agent:codex/t3code-8afe4a1e** at 2026-10-08T22:14:22Z

Deployment record PR #205 is https://git.local.sothr.com/terva-sh/lampi/pulls/205. Clean review on fe28b7aa1c4cb79454efe23d13ca818bd750f26c, base 92c2dbec1e9f04d0bdfc14f78213b8a1124749fc, is https://git.local.sothr.com/terva-sh/lampi/pulls/205#issuecomment-28248 (Actions 1941, run 641fd20d-b06f-46c3-bedb-fb5119467a43), with no findings. It reviews repository ticket records only; the external operator bundle is covered by this session's source inspection, checksums, shell syntax check, and synthetic rehearsal. Carry the verdict for later ticket-only updates.

The prepared root command was requested from the owner. No completion report exists yet and the live binary remains v0.8.0 with both services active. Deployment remains blocked on that operator execution, not on permission: the owner's deployment request already authorizes the change. Preserve the unticked live-deployment criteria until the report and independent post-install checks succeed. To resume, inspect the bundle's DEPLOYMENT_RESULT, verify the installed binary hash/version, both health endpoints and authentication guards, and capture's running state/last sync; then record results and close this ticket.

**agent:codex/t3code-8afe4a1e** at 2026-10-08T22:14:22Z

in-progress to blocked: The checksummed, rehearsed operator bundle is ready and deployment is authorized. Root installation is waiting for the owner to run its command because sudo requires a password; this session has no noninteractive root access. Live services remain unchanged on v0.8.0.

**agent:codex/t3code-8afe4a1e** at 2026-10-08T22:17:25Z

blocked to in-progress: The owner started the prepared root operator script; backup/install verification is now underway.

**agent:codex/t3code-8afe4a1e** at 2026-10-08T22:17:25Z

The prepared root operator script is now running. Both lake and capture units are paused and the installed binary is still v0.8.0, consistent with the stopped-lake checkpoint stage. The sudo execution blocker is resolved by the owner running the command; live verification criteria remain unticked until the script completion report and independent post-install checks succeed.
