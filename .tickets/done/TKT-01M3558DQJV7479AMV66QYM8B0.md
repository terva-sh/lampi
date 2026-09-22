---
schema: 3
id: TKT-01M3558DQJV7479AMV66QYM8B0
title: "Acceptance: query known prompt substring"
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area/search
  - area/ci
  - phase/1-mvp
assignees: []
milestone: mvp
parent: TKT-01M3558DM7F4PV6QVHXR8CGFVG
origin: null
dependencies:
  - TKT-01M3558DNF9KDSAGCB5FFA7JSM
  - TKT-01M3558DPVCN661600P3F9HMB0
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:11Z
updated_at: 2026-09-22T23:42:25Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/66b0
  name: Cursor cloud agent
extensions: {}
---

## Description

Architecture §7 test #5 — after ingest, query normalized text for a fixture prompt.

## Acceptance criteria

- [x] After ingest, a query finds a known fixture prompt in normalized text

## Implementation plan

A Go test puts a fixture terva transcript through the ingest API, runs terva-lampi export, and queries content_text with sqlite for the fixture prompt. The other four architecture section 7 checks stay on TKT-01M3558DPVCN661600P3F9HMB0 (CI/integration: five MVP acceptance tests).

## Notes

**agent:cursor/66b0** at 2026-09-22T23:42:25Z

The dependency on TKT-01M3558DPVCN661600P3F9HMB0 (CI/integration: five MVP acceptance tests) is still open. This ticket only covers architecture section 7 test 5, as a CI test of normalize plus export. Tests 1 through 4 stay on that other ticket.

## Summary

internal/cli TestKnownPromptAfterIngest puts a fixture transcript through the ingest API, runs terva-lampi export, and queries content_text with sqlite for "normalize-proof prompt: lampi-pond-7f3a".

The other four architecture section 7 checks stay on TKT-01M3558DPVCN661600P3F9HMB0 (CI/integration: five MVP acceptance tests). This ticket's dependency on that one is still open; the known-prompt check does not wait on it.
