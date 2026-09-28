---
schema: 3
id: TKT-01M3M7M0PMGW8JG0Y45RFPY34M
title: "Policy: agent inventory reports, SOCIABLE default and STRICT mode"
type: task
status: done
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
updated_at: 2026-09-28T15:13:36Z
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

### STRICT sends aggregate counts (owner, 2026-09-28)

STRICT agents send the aggregate refused count and the total bytes refused, with no names, paths, remotes or hashes. The counts let an operator decide whether to look at the machine: a handful of refused sessions is expected, while hundreds suggest a misconfigured allowlist.

## Acceptance criteria

- [x] docs/policy.md records the 2026-09-28 decision and the two modes
- [x] docs/allowlist-and-redaction.md says what leaves the machine in each mode
- [x] The open question on STRICT's aggregate count is answered

## Implementation plan

Docs only. Add an 'Off-box metadata: the inventory report' section to docs/policy.md after 'Off-box raw', with the dated decision, the two modes, why the mode is local-only, and the rejected alternatives. Add a sentence to 'Base configuration' saying a profile cannot set the mode. Add a 'What leaves the machine' table to docs/allowlist-and-redaction.md, and add an upgrade note to deploy/README.md. The config key is named here as "inventory": "strict" so TKT-01M3M7M0TH implements that exact spelling.

## Notes

**agent:claude-code/2cf53976** at 2026-09-28T15:00:28Z

Owner decision 2026-09-28: STRICT sends aggregate refused counts with no names. A few refused sessions is expected; hundreds suggests a misconfigured allowlist, so the count is enough to prompt an operator to look. Description updated; AC 3 ticked.

## Summary

docs/policy.md now records the 2026-09-28 decision: SOCIABLE by default, STRICT set only in the local config.json as "inventory": "strict", and STRICT still sends the aggregate refused count and bytes. docs/allowlist-and-redaction.md has a per-mode table of what leaves the machine, and deploy/README.md tells operators to set STRICT before upgrading. The docs say the report ships with TKT-01M3M7M0TH; until then agents send nothing about refused projects.
