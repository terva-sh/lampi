---
schema: 3
id: TKT-01M56NN116DEAN872R5BM126SE
title: Prepare v0.9.0 workstation and fleet update scripts
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
  - area/agent
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:codex/memory-e2fc465b
  branch: t3code/explore-lampi-memory-footprint
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-e2fc465b
  commit: 9b0d0ea7ec3145b501102bf88fce37e2202a49f3
  session: null
  claimed_at: 2026-10-18T04:52:30Z
  expires_at: null
archive: null
created_at: 2026-10-18T04:52:20Z
updated_at: 2026-10-18T04:52:30Z
created_by:
  id: agent:codex/memory-e2fc465b
  name: ""
updated_by:
  id: agent:codex/memory-e2fc465b
  name: ""
extensions: {}
---

## Description

The owner asked to prepare update scripts for this machine and the remaining capture agents after completing v0.9.0 publication. This request authorizes preparing and validating the scripts, not running them against live services. Use the established local deployment handoff for machine coordinates; do not guess remote SSH hosts or accounts. Preserve all capture policies, lake pins, identities and credentials. Keep host-specific scripts and any credential-bearing backups outside the repository under the documented handoff boundary, with durable rationale and credential-free commands in this ticket.

## Acceptance criteria

- [ ] A checksummed workstation lake update package pins the published release and preserves backup, health/auth/schema/identity checks and rollback.
- [ ] A version-pinned per-agent update script preserves config/token/state, verifies lake compatibility and the installed release, and documents fleet rollout and rollback.
- [ ] Scripts pass syntax and isolated rehearsal checks; no live update is executed and the package location and commands are recorded.

## Implementation plan

After publication, download and verify all release archives. Adapt the established root-run local lake bundle for the current installed fe787a4 dogfood binary, retaining strict service/config preconditions, stopped-lake backup/fsck/counts/schema/identity checks, atomic installation, authentication/public health checks, and automatic binary rollback. Prepare a portable per-agent Python orchestrator for Linux systemd user units and macOS launchd using explicit or documented config/state/binary paths. Check all configured lakes run at least v0.9.0, take private verified stopped-agent backups, install the checksum-verified released binary atomically, preserve .prev and configuration, resume the prior service state, and verify a fresh successful sync. Provide dry-run/rollback modes, no guessed SSH inventory, and a fleet canary/serial rollout runbook. Validate with synthetic fake service/CLI fixtures, syntax checks, release archive smoke checks and existing local backup rehearsal. Publish the credential-free reusable script through a PR and retain local host coordinates only in the external handoff. Execute no live rollout.
