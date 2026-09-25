---
schema: 3
id: TKT-01M3B369A74CEB0MPSVH2MAHQV
title: "Redaction v2: JSON-escaped context, rule gaps, base64 values"
type: bug
status: ready
status_reason: null
priority: urgent
due_on: null
labels:
  - area/redact
  - area/adapter
assignees: []
milestone: null
parent: TKT-01M3B35JS8J2F83FG4J7ZC0199
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-25T01:34:31Z
updated_at: 2026-09-25T01:34:31Z
created_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
updated_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
extensions: {}
---

## Description

Ruleset v1 misses secrets in the context where transcripts hold them: inside JSON strings.

### Findings

- Escapes defeat `\b`. Proven. The scan runs on raw JSONL, where a line break in a string is `\` then `n`. `n`, `t`, and `r` are word characters, so the leading `\b` on most rules in `internal/redact/redact.go:59-81` fails. A key after `\n` or `\t` scans clean and uploads. `Strip` has the same gap on tool arguments kept as raw JSON text, so the key reaches ShareGPT rows.
- Other gaps. Proven. `AWS_SECRET_ACCESS_KEY=\"…\"` inside JSON, `BEGIN PGP PRIVATE KEY BLOCK`, a Google key ending in `-` (trailing `\b`), and Slack `xapp-`. `Strip` of a JSON-escaped PEM key replaces the BEGIN line and leaves the body.
- Base64 hides secrets. Proven. The Cursor adapters export non-UTF-8 values as `{"base64":…}` (`cursorcli/snapshot.go:327`, `cursor/snapshot.go:444`), and the upload scan only sees the export.
- The AWS documentation example key `AKIAIOSFODNN7EXAMPLE` is a hit, so a session that read SDK docs is quarantined for good.

### Approach

Scan a length-preserving view in which JSON escapes are padded, and map spans back. Replace the trailing `\b` on fixed-length rules. Allow ` BLOCK` in the PEM header and add `xapp-`. Scan raw row values in the Cursor adapters before encoding. Skip a short list of published example values. Ship this as ruleset `v2`, so the manifest stamp tells old scans from new. A table test covers every rule in escaped context.

## Acceptance criteria

- [ ] Every rule matches its secret after a JSON escape; a table test covers it
- [ ] PGP private key blocks, xapp- tokens, and a Google key ending in - are matched
- [ ] Strip removes the whole of a JSON-escaped PEM key
- [ ] Cursor raw values are scanned before base64 encoding
- [ ] Manifests stamp ruleset v2
