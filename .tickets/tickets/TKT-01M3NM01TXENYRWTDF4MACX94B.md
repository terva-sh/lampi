---
schema: 3
id: TKT-01M3NM01TXENYRWTDF4MACX94B
title: Consolidate the internal lake's default profile
type: chore
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
  - policy
assignees: []
milestone: null
parent: TKT-01M3NM01J4731BSWEA815FWGXS
origin: null
dependencies:
  - TKT-01M3NM01Q9PCPWKTW0G2AP2QYT
  - TKT-01M3NM01S2TXXMRSEKFGD1TH10
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T03:40:37Z
updated_at: 2026-09-29T03:41:09Z
created_by:
  id: agent:claude-code/58fb7d84
  name: ""
updated_by:
  id: agent:claude-code/58fb7d84
  name: ""
extensions: {}
---

## Description

The internal lake's `default` profile holds about 97 allow rules, nearly all added one project at a time by Allow. Once the tools under TKT-01M3NM01J (Concise profile rules) are on the lake, use them to fold it down, and record what changed.

### Steps

1. Read the profile read-only (`GET /api/web/v1/profiles/default`) and group the rules by host and owner, and by cwd root.
2. In the editor, remove covered rules and fold owner groups into prefixes. Use `cwd_glob` for per-machine cwd rules where one layout repeats.
3. Before saving, check the preview's admitted list. Every project it admits must be one the operator wants. Anything else stays as an exact rule, or goes on a deny rule.
4. The operator saves with a note naming this ticket. An agent does not save a live profile on its own.

Record the before and after rule counts and the admitted list in a note here. The profile's revision history holds the documents themselves.

Needs a dashboard session with the `operator` role, and a lake running a release that carries the tools above.

## Acceptance criteria

- [ ] The rules are grouped and a consolidated document is drafted in the editor
- [ ] The operator reviewed the admitted list and saved with a note naming this ticket
- [ ] A note records the rule counts before and after
