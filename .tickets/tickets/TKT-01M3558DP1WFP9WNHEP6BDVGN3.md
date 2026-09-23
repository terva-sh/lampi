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
updated_at: 2026-09-23T01:23:40Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/8319
  name: Cursor cloud agent
extensions: {}
---

## Description

Architecture §7 five acceptance tests in CI; operator status command; optional daemon units / lampi alias / hook example.

## Acceptance criteria

- [x] All five MVP acceptance tests green in CI
- [x] `terva-lampi status` reports agent + server essentials

## Definition of done

- [x] All children of this epic are resolved; low-priority optional tickets may stay draft
- [x] TKT-01M3558DPVCN661600P3F9HMB0 CI/integration: five MVP acceptance tests
- [x] TKT-01M3558DRA1KS28WAVBKPY3SH8 Implement terva-lampi status (agent + server)
- [x] TKT-01M3558DS31QVJ6Y6XH6HK8S4V systemd user unit + launchd agent examples
- [x] TKT-01M3558DSRBXEECGW3HCFWX6AA Optional lampi alias installer with neurobin warn
- [x] TKT-01M3558DTHC06337Q51V6ERPTD Example terva post_tool_use enqueue hook
- [x] TKT-01M3558DD3MYA65Q909VDV374P Chunker for large artifacts (≥32 MiB)

## Notes

**agent:cursor/8319** at 2026-09-23T01:23:40Z

Four children landed here: systemd and launchd examples, the optional lampi alias installer, the post_tool_use hook, and the chunker for files over max_blob_bytes. TKT-01M3558DPVCN661600P3F9HMB0 (CI/integration: five MVP acceptance tests) is done; main moved it in #13. Every child of this epic is done. Phase 0 policy tickets were not changed. The deploy examples keep a loopback placeholder because the lake host is not chosen.
