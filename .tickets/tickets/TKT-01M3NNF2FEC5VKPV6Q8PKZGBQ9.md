---
schema: 3
id: TKT-01M3NNF2FEC5VKPV6Q8PKZGBQ9
title: "Bays: inbox, bulk move, apply-rules and the sorting guide"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/ops
  - area/docs
assignees: []
milestone: null
parent: TKT-01M3N8KHW56GVZXHV0KBNSPEX1
origin: null
dependencies:
  - TKT-01M3NNF29WCV5F1D8QSBWPM6M0
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T04:06:17Z
updated_at: 2026-09-29T14:58:27Z
created_by:
  id: agent:claude-code/7859b064
  name: ""
updated_by:
  id: agent:claude-code/7859b064
  name: ""
extensions: {}
---

## Description

Tooling for keeping the default bay at zero. Design in the parent epic TKT-01M3N8KHW5 (Bays: segment one lake and route sessions to a bay).

### Scope

- `serve bays inbox` lists sessions in `default`, held sessions, and sessions with a refused request, each with its reason: no rule matched, request refused (which bay, why), or hold rule X.
- Bulk move by filter (project, git remote, cwd prefix, device, harness) with `--dry-run`, audited.
- `serve bays apply-rules` runs the lake rules again over stored sessions, add-only as at ingest, with `--dry-run`.
- `serve bays release` for a held session restores its requested bays.
- A guide in `docs/` covering how to sort an existing lake after upgrade, how to write rules that keep new sessions out of the inbox, and why the aim is an empty inbox.

## Acceptance criteria

- [ ] serve bays inbox gives a reason for every listed session
- [ ] Bulk move and apply-rules have --dry-run and audit every change
- [ ] The sorting guide is in docs/
