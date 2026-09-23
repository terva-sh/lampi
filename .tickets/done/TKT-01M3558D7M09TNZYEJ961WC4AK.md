---
schema: 3
id: TKT-01M3558D7M09TNZYEJ961WC4AK
title: Decide lake host (NAS / VPS / S3+index VM)
type: task
status: done
status_reason: null
priority: high
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

Honest unknown from architecture: where the session lake runs. Affects encryption story, auth exposure, and daemon packaging.

## Implementation plan

Record the locked host in docs/policy.md and point docs/architecture.md and deploy/ at it. The lake is terva-lampi serve on a small VPS with local disk. Example units keep the loopback URL and tell operators to set LAMPI_SERVER to the VPS HTTPS URL. No provisioning, TLS terminator, LUKS script, or object-store backend.

## Notes

**human:sothr** at 2026-09-22T18:15:12Z

Options: home NAS, VPS, or S3-compatible object store + small index VM.

**agent:cursor/1b27** at 2026-09-23T03:08:06Z

Decision from human:sothr: VPS. terva-lampi serve runs on a small VPS with local disk. Not a home NAS, and not S3 plus an index VM, for the MVP. TLS sits in front of serve; plain HTTP stays off any public interface. Device tokens stay the implemented --token-file path, hashed at rest on the server. The tenant is Drew's machines. Recorded in docs/policy.md. Example units keep the loopback placeholder and point operators at that policy.

## Summary

Lake host is a small VPS running terva-lampi serve on local disk. docs/policy.md records it, including TLS in front of serve, hashed device tokens, and the single-tenant limit. docs/architecture.md, README.md, and deploy/ now point at that policy. The example units still default to the loopback URL.
