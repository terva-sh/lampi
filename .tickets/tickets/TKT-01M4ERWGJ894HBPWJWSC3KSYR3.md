---
schema: 3
id: TKT-01M4ERWGJ894HBPWJWSC3KSYR3
title: Deploy dashboard pagination and maintenance to dogfooding
type: task
status: in-progress
status_reason: null
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
  commit: 92c2dbec1e9f04d0bdfc14f78213b8a1124749fc
  session: null
  claimed_at: 2026-10-08T22:07:27Z
  expires_at: null
archive: null
created_at: 2026-10-08T22:07:04Z
updated_at: 2026-10-08T22:07:27Z
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

- [ ] A checksummed deployment binary and operator script are ready with rollback and verified backup gates.
- [ ] The dogfooding lake runs the merged build with schema, identity, counts, health, and authentication guards preserved.
- [ ] The public site responds and capture resumes successfully; the deployment outcome is recorded.

## Implementation plan

Build the synchronized and gated main commit as a clearly labeled dogfooding binary without publishing a release tag. Adapt the proven root-run deployment script with strict installed-binary/unit/role/schema checks, a verified stopped-lake checkpoint, atomic binary install, immediate rollback on a failed install check, and restoration of capture state. Rehearse backup/fsck and old-to-new startup on isolated synthetic data, and checksum the complete external operator bundle. Ask the owner to run only the prepared root command because noninteractive sudo is unavailable. Verify running version, loopback/public health and auth guards, schema/counts/identity checks from the operator report, resumed capture, and the added UI when an authenticated browser is available. Record the result in this ticket and leave publication of v0.9.0 separate.

## Notes

**agent:codex/t3code-8afe4a1e** at 2026-10-08T22:07:27Z

draft to ready: Owner explicitly requested this dogfooding deployment on 2026-10-08.
