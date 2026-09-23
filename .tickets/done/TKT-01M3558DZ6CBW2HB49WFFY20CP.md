---
schema: 3
id: TKT-01M3558DZ6CBW2HB49WFFY20CP
title: Phase 3 export-based harnesses & conflicts
type: epic
status: done
status_reason: null
priority: low
due_on: null
labels:
  - area/adapter
  - phase/3-export
assignees: []
milestone: phase-3
parent: null
origin: null
dependencies:
  - TKT-01M3558DV5NHYZBFMPV1AVRNYQ
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:12Z
updated_at: 2026-09-23T20:22:05Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/f860
  name: Cursor cloud agent
extensions: {}
---

## Description

OpenCode via export schedule; optional terva sidecars; divergent_copy dashboard; optional git-ticket↔session_uid glue.

## Definition of done

- [x] All children of this epic are done or parked
- [x] TKT-01M3558DZXWZWN1SEFHN2SF6S4 OpenCode via scheduled opencode export
- [x] TKT-01M3558E0M4CT3DWR4Z4GEY584 Optional sidecars: terva raati/ and tasks archives
- [x] TKT-01M3558E1DAWTFQF88M9J6NJR6 Conflict dashboard for divergent_copy
- [ ] TKT-01M3558E21PZDJEYAAR4WH9B85 Soft-link git-ticket claims to lake session_uid

## Notes

**agent:cursor/c937** at 2026-09-23T17:30:24Z

Promoted with three children whose dependencies are already done: TKT-01M3558DZXWZWN1SEFHN2SF6S4 (OpenCode via scheduled opencode export), TKT-01M3558E0M4CT3DWR4Z4GEY584 (Optional sidecars: terva raati/ and tasks archives), and TKT-01M3558E1DAWTFQF88M9J6NJR6 (Conflict dashboard for divergent_copy). Those three tasks gained acceptance criteria taken from their descriptions.

TKT-01M3558E21PZDJEYAAR4WH9B85 (Soft-link git-ticket claims to lake session_uid) stays draft. Its description says to park until the Phase 3 need is clear, and this promote did not establish that need. Phase 4 (TKT-01M3558E2PNBKNHFY6J7A37GJ4) and Phase 5 (TKT-01M3558E4TN25NXXB0XP127RVK) stay draft with their children.

**agent:cursor/f860** at 2026-09-23T20:22:05Z

The last ready child, TKT-01M3558E1DAWTFQF88M9J6NJR6 (Conflict dashboard for divergent_copy), is done. OpenCode and the terva sidecars were already done.

TKT-01M3558E21PZDJEYAAR4WH9B85 (Soft-link git-ticket claims to lake session_uid) stays draft. Its description says to park until the Phase 3 need is clear, and closing this epic did not establish that need. The definition-of-done line that names that ticket stays unchecked because the work was not done. The first line is checked because every child is done or parked.

## Summary

Phase 3 is done. OpenCode export JSON is discovered and uploaded, optional terva raati records and tasks archives attach to the session they belong to, and divergent_copy rows can be listed from the catalog with session_uid, artifact id, both digests, and provenance machines.

TKT-01M3558E21PZDJEYAAR4WH9B85 (Soft-link git-ticket claims to lake session_uid) stays draft. That glue was parked on purpose and is not part of this close.
