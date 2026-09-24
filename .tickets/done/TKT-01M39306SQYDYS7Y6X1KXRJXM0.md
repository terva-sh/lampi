---
schema: 3
id: TKT-01M39306SQYDYS7Y6X1KXRJXM0
title: Allowlist DX notes for Cursor cwd refuses
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/docs
  - area/ops
assignees: []
milestone: null
parent: TKT-01M39306SFZNBM2YE2TJ49S7PP
origin: null
dependencies:
  - TKT-01M38RJCDREDTTPTY2D7SR8W59
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-24T06:53:15Z
updated_at: 2026-09-24T08:29:26Z
created_by:
  id: agent:atlas/architect
  name: Atlas - Architect
updated_by:
  id: agent:cursor/6dc0
  name: Cursor cloud agent
extensions: {}
---

## Description

Document (and optionally surface in status) why Cursor IDE global state and Cursor CLI exports without an absolute cwd are refused by the existing default-deny allowlist. No allowlist policy change.

## Acceptance criteria

- [x] Docs state that Cursor IDE global DB has empty cwd and is refused by design; workspace DB cwd comes from workspace.json
- [x] Docs state that Cursor CLI needs absolute cwd in sibling meta (or equivalent) or the export is refused
- [x] Optional: status or sync stderr hint points operators at those cases without changing permit rules
- [x] `projects` allow/deny schema and default-deny behavior are unchanged

## Definition of done

- [x] Docs (and optional status hint) merged
- [x] No allowlist rule engine changes beyond messaging

## Implementation plan

State the existing default-deny outcome for two Cursor exports that have no project path. Leave `projects` allow/deny matching in `internal/config` as it is.

README Off-box raw, docs/policy.md, docs/architecture.md, and docs/protocol.md say the Cursor IDE global database has an empty cwd and is refused by design, and that a workspace database takes its cwd from workspace.json. The same places say a Cursor CLI export needs an absolute cwd in the sibling meta.json (a relative path or a file URI is not one) or the allowlist refuses it. `sync` and `agent` help text say the same.

When a sync already refuses a `cursor` or `cursor-cli` session whose cwd is empty, the refusal line gains that explanation. `status` help points at the same cases. Permit rules, the schema, and which sessions match stay unchanged.

## Summary

README, docs/policy.md, docs/architecture.md, and docs/protocol.md state that the Cursor IDE global database has an empty cwd and is refused by design, and that a workspace database takes its cwd from workspace.json. They also state that a Cursor CLI export needs an absolute cwd in the sibling meta.json. A missing file, a relative path, or a file URI is an empty cwd, and the allowlist refuses that export.

### Operator hint

`sync` and `agent` help text say the same. `status` help points at those two cases. When a sync already refuses a `cursor` or `cursor-cli` session whose cwd is empty, the stderr line names which case it is. A session that has a cwd keeps the existing refusal line.

### Unchanged

`projects` allow and deny, `Projects.Permitted`, and default deny are unchanged. `go test ./...` passed.
