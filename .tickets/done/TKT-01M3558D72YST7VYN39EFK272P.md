---
schema: 3
id: TKT-01M3558D72YST7VYN39EFK272P
title: Phase 0 policy & lake placement
type: epic
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
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:11Z
updated_at: 2026-09-23T03:08:19Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/1b27
  name: Cursor cloud agent
extensions: {}
---

## Description

Settle retention, encryption at rest, project allowlist for off-box raw, lake host, and machine inventory before raw session bytes leave any machine.

## Acceptance criteria

- [x] Lake host decision recorded in docs
- [x] Retention + encryption policy committed
- [x] Allowlist config surface agreed
- [x] Machine list confirmed (or explicitly deferred)

## Definition of done

- [x] Child policy tickets closed or parked with `question` label
- [x] MVP epics unblocked on allowlist at minimum

## Implementation plan

After docs/policy.md lands, close the three open children and tick this epic. Acceptance is the host decision, the retention and encryption policy, the already-shipped allowlist surface, and the three agent roles. Definition of done is those children done, plus the MVP path already unblocked by the allowlist in TKT-01M3558D8WN5HTVPM4KRQQCSHP.

## Notes

**agent:cursor/1b27** at 2026-09-23T03:08:19Z

Children are done. TKT-01M3558D7M09TNZYEJ961WC4AK (Decide lake host) records the VPS. TKT-01M3558D8AVEAA1565Y6KRNJTE (Write retention + encryption-at-rest policy) records no TTL, volume encryption and/or LUKS, and confirms the existing allowlist. TKT-01M3558D9E0NJVHYNK0S4HQJ0N (Confirm machine inventory) records laptop, desktop, and a remote/cloud box. TKT-01M3558D8WN5HTVPM4KRQQCSHP (Define project allowlist for off-box raw) was already done and is the config surface this epic required. The MVP epic is already unblocked on that allowlist. docs/policy.md is the writeup.

## Summary

Phase 0 is recorded in docs/policy.md and the four children are done. The lake is terva-lampi serve on a small VPS with local disk, TLS in front, no TTL, and provider volume encryption and/or LUKS. Off-box raw stays the existing default-deny allowlist. Laptop, desktop, and a remote/cloud box run terva-lampi agent. Architecture and deploy notes point at the policy and keep the loopback example URL.
