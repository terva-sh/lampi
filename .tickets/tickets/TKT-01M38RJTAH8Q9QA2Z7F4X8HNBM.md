---
schema: 3
id: TKT-01M38RJTAH8Q9QA2Z7F4X8HNBM
title: OpenCode normalize projector
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/normalize
assignees: []
milestone: null
parent: TKT-01M38RJCDREDTTPTY2D7SR8W59
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-24T03:50:38Z
updated_at: 2026-09-24T03:51:20Z
created_by:
  id: agent:cursor/dcb2
  name: Cursor cloud agent
updated_by:
  id: agent:cursor/dcb2
  name: Cursor cloud agent
extensions: {}
---

## Description

Project a stored OpenCode export document into schema_version 1 events.

The intended bytes are one `opencode export` JSON document. The adapter uploads it as artifact kind `transcript_jsonl`, harness `opencode`, even though the file is one JSON document rather than JSONL. `internal/adapter/opencode` reads `info.id` and `info.directory`. Every other key stays on `Record.Extra` or `Info.Extra`. `harness_version` is the adapter's pinned reader version.

When `export/` has no JSON, discovery falls back to a `.db` file at the data-directory root and uploads that file as `transcript_jsonl` with an empty project. That blob is a SQLite database, not an export document. This projector parses the export document. A stored database blob keeps `normalize_error`. The WAL sidecar is not a session.

`session_id` is `opencode:` plus the native session id, the same shape terva uses.

`api.Server.Project` currently rejects harness `opencode` before it reads the blob. This ticket extends that branch so an opencode export uses an OpenCode projector.

### Fixture

Use an export document that includes a user or assistant turn, one unknown key, and an `encrypted_content` value when the document can carry one. A body that is not a JSON object fails. The error text does not include the body. A `.db` blob is a failure fixture, not a success fixture.

## Acceptance criteria

- [ ] A stored opencode export JSON document (artifact kind transcript_jsonl) projects to schema_version 1 events. session_id is opencode: plus the native session id. A .db fallback blob keeps normalize_error. The CAS object is not opened for write
- [ ] Unknown keys on the stored record are kept. encrypted_content, when the fixture carries it, is copied as an opaque value, is left out of content_text, and is not decrypted. The projector does not invent secret or credential values
- [ ] A worker given a success fixture of this harness clears normalize_error, writes normalized/<session_uid>.jsonl, and writes a parquet file at parquet/date=YYYY-MM-DD/harness=<harness>/<session_uid>.parquet. The date is the UTC day of recorded_at, or ingested_at when recorded_at is missing. A projection failure sets normalize_error, removes that session's derived JSONL and parquet, and leaves the CAS object in place
- [ ] terva-lampi export --format events writes the projected events. When the fixture has a training turn (message, tool call, tool result, compaction, or error) and the session is allowlisted, --format sharegpt and --format trajectory follow the existing rules: content_text becomes value; meta, usage, and unknown rows are left out; encrypted_content stays opaque and out of value; ruleset v1 strips plaintext training fields; the CAS object and the normalized JSONL are not rewritten. A fixture with no training turn is named on stderr and omitted
- [ ] go test ./... is green, including a unit or accept-style OpenCode export fixture. TestUnimplementedHarnessRecordsNormalizeError no longer expects a valid OpenCode export to fail

## Definition of done

- [ ] The projector lives in internal/normalize and api.Server.Project calls it for this harness and its artifact kind
- [ ] The harness adapter is unchanged. It still uploads the same bytes and still drops or keeps the same keys
- [ ] The terva projector still projects a terva transcript, and go test ./... passes
