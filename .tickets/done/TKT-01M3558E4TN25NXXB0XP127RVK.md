---
schema: 3
id: TKT-01M3558E4TN25NXXB0XP127RVK
title: Phase 5 training export
type: epic
status: done
status_reason: null
priority: low
due_on: null
labels:
  - area/normalize
  - phase/5-train
assignees: []
milestone: phase-5
parent: null
origin: null
dependencies:
  - TKT-01M3558DM7F4PV6QVHXR8CGFVG
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:12Z
updated_at: 2026-09-23T23:20:40Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/3355
  name: Cursor cloud agent
extensions: {}
---

## Description

Allowlisted ShareGPT/trajectory export with raw_sha256 lineage; secret strip on training view only.

## Definition of done

- [x] All children of this epic are done
- [x] TKT-01M3558E5QS1JN6B7HDATCCC2C Allowlisted trajectory / ShareGPT export
- [x] TKT-01M3558E6GGK5KX8766957WTQR Strip secrets in training view (normalized only)

## Notes

**agent:cursor/cbf6** at 2026-09-23T22:21:37Z

Promoted with two children. TKT-01M3558E5QS1JN6B7HDATCCC2C (Allowlisted trajectory / ShareGPT export) and TKT-01M3558E6GGK5KX8766957WTQR (Strip secrets in training view (normalized only)) gained acceptance criteria taken from their descriptions. The secret-strip task depends on the export task and is not startable until that export is done.

Dependencies outside this phase are done: TKT-01M3558DM7F4PV6QVHXR8CGFVG (MVP normalize + search proof) for this epic; TKT-01M3558D8AVEAA1565Y6KRNJTE (Write retention + encryption-at-rest policy), TKT-01M3558D8WN5HTVPM4KRQQCSHP (Define project allowlist for off-box raw), and TKT-01M3558DMTAXM5GGN2QW0C728R (Normalize terva raw → schema_version 1 events) for the export; TKT-01M3558DCDJ71TN19N5DDY4RSF (Redaction ruleset v1 + quarantine on hits) for the secret strip.

TKT-01M3558E21PZDJEYAAR4WH9B85 (Soft-link git-ticket claims to lake session_uid) stays draft. Its description says to park until the Phase 3 need is clear, and this promote did not establish that need.

**agent:cursor/a72e** at 2026-09-23T22:43:46Z

TKT-01M3558E5QS1JN6B7HDATCCC2C (Allowlisted trajectory / ShareGPT export) is done. TKT-01M3558E6GGK5KX8766957WTQR (Strip secrets in training view (normalized only)) is not done. The epic stays open until that child lands.

**agent:cursor/3355** at 2026-09-23T23:20:26Z

TKT-01M3558E6GGK5KX8766957WTQR (Strip secrets in training view (normalized only)) is done. Both children of this epic are done.

TKT-01M3558E21PZDJEYAAR4WH9B85 (Soft-link git-ticket claims to lake session_uid) stays draft.

## Summary

Phase 5 is done. Allowlisted ShareGPT and trajectory export keeps raw_sha256 lineage and opaque encrypted_content. The training view strips ruleset v1 matches from plaintext fields. The CAS and `--format events` are not rewritten.

TKT-01M3558E21PZDJEYAAR4WH9B85 (Soft-link git-ticket claims to lake session_uid) stays draft.
