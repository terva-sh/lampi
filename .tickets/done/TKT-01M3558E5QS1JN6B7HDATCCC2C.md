---
schema: 3
id: TKT-01M3558E5QS1JN6B7HDATCCC2C
title: Allowlisted trajectory / ShareGPT export
type: task
status: done
status_reason: null
priority: low
due_on: null
labels:
  - area/normalize
  - phase/5-train
assignees: []
milestone: phase-5
parent: TKT-01M3558E4TN25NXXB0XP127RVK
origin: null
dependencies:
  - TKT-01M3558D8AVEAA1565Y6KRNJTE
  - TKT-01M3558D8WN5HTVPM4KRQQCSHP
  - TKT-01M3558DMTAXM5GGN2QW0C728R
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:12Z
updated_at: 2026-09-23T22:43:35Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/a72e
  name: Cursor cloud agent
extensions: {}
---

## Description

Export filtered datasets with raw_sha256 lineage; keep opaque encrypted_content opaque.

## Acceptance criteria

- [x] Export is an allowlisted trajectory / ShareGPT dataset
- [x] The export keeps raw_sha256 lineage
- [x] Opaque encrypted_content stays opaque

## Implementation plan

`terva-lampi export` keeps `--format events` as the existing normalized JSONL. That path is unchanged: every session whose normalize succeeded, no allowlist, one schema_version 1 object per line.

`--format sharegpt` and `--format trajectory` are one training projection. Each output line is one session in ShareGPT shape (`conversations` of `from` / `value`). Turns are message, tool call, tool result, compaction, and error. Meta, usage, and unknown rows without text are omitted. A tool call keeps `name` and `call_id`, and its `value` is `content_text`.

The `projects` allowlist in config.json gates that file. Default deny, and deny wins. The check uses the stored manifest cwd, cwd hash, and git remote. A session that is not permitted is named on stderr and omitted. The command does not rewrite the CAS.

`raw_sha256` on each row is the current `transcript_jsonl` digest. `encrypted_content` is copied from the event extra onto the turn. It is not written into `value` and it is not decrypted.

Secret stripping stays on TKT-01M3558E6GGK5KX8766957WTQR (Strip secrets in training view (normalized only)).

## Summary

`terva-lampi export --format sharegpt` and `--format trajectory` write one ShareGPT conversation per session that config.json allowlists and that has a training turn. `--format events` is unchanged.

Each training row has `raw_sha256`, the current transcript_jsonl digest. `conversations` turns are message, tool call, tool result, compaction, and error. `value` is `content_text`. A tool call keeps `name` and `call_id`. `encrypted_content` is copied onto the turn as stored, is not written into `value`, and is not decrypted. The CAS is not rewritten.

The allowlist is the stored manifest cwd, cwd hash, and git remote. Default deny. Deny wins. A session that is not permitted, that failed normalize, or that has no transcript digest or no training turn is named on stderr and omitted.

Secret stripping is not done. TKT-01M3558E6GGK5KX8766957WTQR (Strip secrets in training view (normalized only)) is the next pass, over this training view only. The Phase 5 epic stays open.
