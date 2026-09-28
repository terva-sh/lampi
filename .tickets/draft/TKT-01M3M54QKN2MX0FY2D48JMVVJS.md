---
schema: 3
id: TKT-01M3M54QKN2MX0FY2D48JMVVJS
title: Lake lock error blames a running serve for any failure
type: bug
status: draft
status_reason: null
priority: low
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
claim: null
archive: null
created_at: 2026-09-28T14:01:47Z
updated_at: 2026-09-28T14:14:21Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

`serve compact` run without `--data` as the service user printed:

    terva-lampi: compact: lakelock: mkdir /home/terva-lampi: permission denied; stop serve first, or pass --dry-run

The default data directory sat under a home that does not exist, and
the lock could not be created. The hint names a running serve, which
was stopped. The lock error should say which lake directory it tried
and add the "stop serve first" hint only when the lock is held, not
for any failure to create it.
