---
schema: 3
id: TKT-01M3558DP1WFP9WNHEP6BDVGN3
title: MVP acceptance, status, ops
type: epic
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area/ops
  - area/ci
  - phase/1-mvp
assignees: []
milestone: mvp
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:11Z
updated_at: 2026-09-22T18:31:21Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: human:sothr
  name: Drew Short
extensions: {}
---

## Description

Architecture §7 five acceptance tests in CI; operator status command; optional daemon units / lampi alias / hook example.

## Acceptance criteria

- [ ] All five MVP acceptance tests green in CI
- [ ] `terva-lampi status` reports agent + server essentials

## Definition of done

- [ ] All children of this epic are resolved; low-priority optional tickets may stay draft
- [ ] TKT-01M3558DPVCN661600P3F9HMB0 CI/integration: five MVP acceptance tests
- [ ] TKT-01M3558DRA1KS28WAVBKPY3SH8 Implement terva-lampi status (agent + server)
- [ ] TKT-01M3558DS31QVJ6Y6XH6HK8S4V systemd user unit + launchd agent examples
- [ ] TKT-01M3558DSRBXEECGW3HCFWX6AA Optional lampi alias installer with neurobin warn
- [ ] TKT-01M3558DTHC06337Q51V6ERPTD Example terva post_tool_use enqueue hook
