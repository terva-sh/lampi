---
schema: 3
id: TKT-01M3M7M0PMGW8JG0Y45RFPY34M
title: "Policy: agent inventory reports, SOCIABLE default and STRICT mode"
type: task
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - policy
  - area/docs
assignees: []
milestone: null
parent: TKT-01M3M7KB32E2BCFEA9CN710522
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T14:45:05Z
updated_at: 2026-09-28T14:45:05Z
created_by:
  id: agent:claude-code/2cf53976
  name: ""
updated_by:
  id: agent:claude-code/2cf53976
  name: ""
extensions: {}
---

## Description

Record the owner's 2026-09-28 decision in `docs/policy.md` and `docs/allowlist-and-redaction.md`. Until now, a project outside the allowlist sent nothing off the machine, not even its name.

### What changes

- **SOCIABLE (default).** The agent reports an inventory to each lake it is registered with: harness, project cwd, git remote, session count, total bytes, newest session time, and whether each project is allowed or refused, with the refusal reason. Raw transcripts of refused projects still stay on the machine. Only this metadata leaves.
- **STRICT.** Set in the local `config.json` only (for example `"inventory": "strict"`). A lake profile cannot set it or clear it, and `forbiddenKeys` rejects it in a profile. The agent reports allowlisted projects only, plus the aggregate counters it already prints. It still fetches and applies profiles.
- An agent upgraded from a release that sent nothing starts reporting in SOCIABLE mode. The release notes and `deploy/README.md` say so, and say how to set STRICT before upgrading.

### Why the mode is local-only

A lake that could switch a machine to SOCIABLE could pull the names of projects the machine's owner chose not to share. The machine's owner is the one who gets to refuse.

### Alternatives considered

- Opt-in (STRICT by default): rejected by the owner. The lake would stay blind on every new machine, which is the failure that prompted this work.
- Hashes only (`cwd_hash`): the dashboard could not show or act on what it sees.
- Keeping the inventory local (`agent refused`): leaves the operator reading logs on each machine.

### Open question

Does STRICT send the aggregate refused count (one number, no names)? Proposed: yes, so the device page can still say "refusing N sessions".

## Acceptance criteria

- [ ] docs/policy.md records the 2026-09-28 decision and the two modes
- [ ] docs/allowlist-and-redaction.md says what leaves the machine in each mode
- [ ] The open question on STRICT's aggregate count is answered
