---
schema: 3
id: TKT-01M3W3QJHS3WQZGV3PA187QFMW
title: "Docs: a deployment page that picks laptop or fleet"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/docs
assignees: []
milestone: null
parent: TKT-01M3W3PZSDCB5FGKJS06MSZH65
origin: null
dependencies:
  - TKT-01M3W3QHYC74DZCQQSDKN4ZVYM
  - TKT-01M3W3QJ674XW4RD3JFDE3PP6W
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-01T16:11:03Z
updated_at: 2026-10-01T16:11:03Z
created_by:
  id: agent:claude-code/0f3154cf
  name: ""
updated_by:
  id: agent:claude-code/0f3154cf
  name: ""
extensions: {}
---

## Description

deploy/README.md is a long reference, and docs/getting-started.md
runs everything in the foreground. Neither tells a new reader which
deployment they want.

Add a short page that asks one question, keeping sessions on this
machine or collecting them from many machines, and sends the reader
to the laptop install or the fleet walkthrough. Link it first from
README.md and docs/README.md. Move material out of deploy/README.md
only where the two walkthroughs now cover it.

## Acceptance criteria

- [ ] README.md and docs/README.md link the page first
- [ ] The page sends each reader to one of the two walkthroughs
