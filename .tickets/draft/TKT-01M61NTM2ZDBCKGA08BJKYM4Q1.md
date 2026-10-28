---
schema: 3
id: TKT-01M61NTM2ZDBCKGA08BJKYM4Q1
title: "Tool usage: top tools, failure rates and a cross-harness map"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
  - area/server
assignees: []
milestone: null
parent: TKT-01M61NT6XQ5WA0XZ0AXZEYMHHG
origin: null
dependencies:
  - TKT-01M61NTM0K2TQHX28T2R4HN0TN
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T16:34:53Z
updated_at: 2026-10-28T16:34:53Z
created_by:
  id: agent:claude-code/d4b24133
  name: ""
updated_by:
  id: agent:claude-code/d4b24133
  name: ""
extensions: {}
---

## Description

Show which tools are used most, how often each fails, and how tools compare across harnesses.

- Add a canonical tool vocabulary (for example shell, edit, read, write, search, web, task, mcp:<server>) with a mapping table from each harness's tool names. Apply it when reading, so changing the map needs no reprojection. Unmapped names show as themselves.
- Views: top tools by call count, failure rate per tool (using the call_id join from the error-trends ticket), and a harness by tool matrix.
- Filter by harness and time range like `/activity`.

## Acceptance criteria

- [ ] Canonical tool map applied at read time
- [ ] Top tools, failure rate per tool and harness by tool matrix
