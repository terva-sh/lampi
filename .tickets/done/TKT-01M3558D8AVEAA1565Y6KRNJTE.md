---
schema: 3
id: TKT-01M3558D8AVEAA1565Y6KRNJTE
title: Write retention + encryption-at-rest policy
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area/ops
  - phase/0-policy
  - policy
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

Document TTL, encryption at rest (age/LUKS/S3 SSE), and whether raw may leave the machine for all vs allowlisted projects.

## Acceptance criteria

- [x] Policy markdown under docs/
- [x] Defaults safe for single-tenant Drew machines

## Implementation plan

Write docs/policy.md with the locked retention and encryption rules, and confirm the existing off-box allowlist. No TTL: keep session data until an explicit manual purge. Encryption at rest is provider volume encryption and/or LUKS on the VPS data disk, with no application-level age wrapping. Off-box raw stays default deny on cwd prefix, git remote, and terva cwd hash. The tenant is Drew's machines.

## Notes

**agent:cursor/1b27** at 2026-09-23T03:08:06Z

Decision from human:sothr: no TTL. Session data stays until an explicit manual purge. Encryption at rest is the provider's volume encryption and/or LUKS on the VPS data disk. No application-level age wrapping for the MVP. Off-box raw stays the existing default-deny allowlist: cwd prefix, git remote, and terva cwd hash, with deny winning. docs/policy.md confirms that surface and does not change it. Defaults are for Drew's machines only.

## Summary

docs/policy.md records no TTL until a manual purge, and encryption at rest as provider volume encryption and/or LUKS, with no application-level age wrapping. The same file confirms the existing default-deny allowlist (cwd prefix, git remote, terva cwd hash) for Drew's machines.
