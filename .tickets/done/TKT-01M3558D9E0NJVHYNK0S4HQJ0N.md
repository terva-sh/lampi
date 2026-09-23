---
schema: 3
id: TKT-01M3558D9E0NJVHYNK0S4HQJ0N
title: Confirm machine inventory (laptop/desktop/remote)
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
  - phase/0-policy
assignees: []
milestone: null
parent: TKT-01M3558D72YST7VYN39EFK272P
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:11Z
updated_at: 2026-09-23T03:08:07Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/1b27
  name: Cursor cloud agent
extensions: {}
---

## Description

List machines that will run `terva-lampi agent` for the multi-host proof (architecture acceptance #4).

## Implementation plan

Record the multi-host proof inventory in docs/policy.md. Laptop, desktop, and a remote/cloud box each run terva-lampi agent. Names stay role-level.

## Notes

**agent:cursor/1b27** at 2026-09-23T03:08:06Z

Decision from human:sothr: laptop, desktop, and a remote/cloud box each run terva-lampi agent for the multi-host proof. Names stay role-level. The lake they upload to is the VPS running terva-lampi serve. Recorded in docs/policy.md.

## Summary

docs/policy.md lists the multi-host proof as three agent roles: laptop, desktop, and a remote/cloud box. Names stay role-level. They upload to the VPS lake.
