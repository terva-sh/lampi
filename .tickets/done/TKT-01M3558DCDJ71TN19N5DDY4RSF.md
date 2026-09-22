---
schema: 3
id: TKT-01M3558DCDJ71TN19N5DDY4RSF
title: Redaction ruleset v1 + quarantine on hits
type: task
status: done
status_reason: null
priority: urgent
due_on: null
labels:
  - area/redact
  - phase/1-mvp
assignees: []
milestone: mvp
parent: TKT-01M3558D9Z1ZWHXB3V14A4AJGR
origin: null
dependencies:
  - TKT-01M3558D8WN5HTVPM4KRQQCSHP
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:11Z
updated_at: 2026-09-22T20:21:17Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: human:sothr
  name: Drew Short
extensions: {}
---

## Description

Local secret regex scan before network. Stamp redaction.ruleset=v1 on artifact metadata. Quarantine hits; allowlisted projects only for raw off-box.

## Acceptance criteria

- [x] Common token/key patterns caught in fixtures
- [x] Hits never uploaded without explicit override path

## Implementation plan

Replace the AllowAll stub in internal/redact with ruleset v1. The scan is regexes for common tokens and private keys. It counts hits and names the rules. It does not keep the matched text.

upload.Sync runs the scan on allowlisted bytes before any network call and stamps redaction.ruleset=v1 on each artifact. A hit is appended to quarantine.jsonl under the state dir (mode 0600) and is not uploaded. redaction.upload_hits in config is the only override that lets a hit leave the machine, and the manifest status is then override with the hit count.

## Summary

internal/redact ruleset v1 scans for private keys and common token shapes and records rule names, not the matched text. upload.Sync runs that scan after the allowlist and before any request. Clean artifacts are stamped redaction.ruleset=v1 and status scanned.

A hit is appended to quarantine.jsonl (mode 0600) and is not uploaded. redaction.upload_hits is the only override; the manifest status is then override and the hit count is kept. Fixtures cover the token patterns, and the quarantine log does not contain the secret.
