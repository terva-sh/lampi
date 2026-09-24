---
schema: 3
id: TKT-01M38RJTC2C4KAGM3SK7ABE1WA
title: Cursor CLI normalize projector
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

Project a stored Cursor CLI export into schema_version 1 events.

The bytes are the filtered JSON the adapter already uploads, artifact kind `cursor_cli_store_json`, harness `cursor-cli`. The document carries `harness_version`, `confidence`, `source`, `scope`, `meta`, and `blobs`. The raw `store.db` is not in the CAS. Meta values may be JSON or hexadecimal that decodes to JSON. Blob bytes are not hex-decoded, and protobuf is not parsed. Reader version is 1 and confidence is low: unknown keys stay on the event.

A key named `cursorAuth`, a key whose first slash-separated segment is `cursorAuth`, and the credential names the adapter already drops (`accessToken`, `refreshToken`, `idToken`, `sessionToken`, the underscore forms, and `workosCursorSessionToken`) are already absent. The projector leaves them absent. It does not invent credential values.

`session_id` is `cursor-cli:` plus the native session id, the same shape terva uses.

The IDE document is a different shape (`cursor_state_json`, ItemTable and cursorDiskKV, harness `cursor`). This ticket does not call the IDE projector and does not assume the two stores match.

`api.Server.Project` only reads `transcript_jsonl` and `errors_jsonl`. A cursor-cli manifest's artifact is `cursor_cli_store_json`. This ticket reads that kind. Publishing an empty event list because the kind was skipped is a failure: the worker sets `normalize_error` and writes no derived files.

### Fixture

Use a `cursor_cli_store_json` document with a conversation row, one unknown key, and an `encrypted_content` value when a row can carry one. A `cursor_state_json` blob, or a `transcript_jsonl` blob, on a cursor-cli session is a failure fixture.

## Acceptance criteria

- [ ] A stored cursor_cli_store_json export projects to schema_version 1 events. session_id is cursor-cli: plus the native session id. A cursor_state_json or transcript_jsonl blob on a cursor-cli session is a failure. The CAS object is not opened for write
- [ ] Unknown keys on the stored record are kept. encrypted_content, when the fixture carries it, is copied as an opaque value, is left out of content_text, and is not decrypted. The projector does not invent secret or credential values
- [ ] A worker given a success fixture of this harness clears normalize_error, writes normalized/<session_uid>.jsonl, and writes a parquet file at parquet/date=YYYY-MM-DD/harness=<harness>/<session_uid>.parquet. The date is the UTC day of recorded_at, or ingested_at when recorded_at is missing. A projection failure sets normalize_error, removes that session's derived JSONL and parquet, and leaves the CAS object in place
- [ ] terva-lampi export --format events writes the projected events. When the fixture has a training turn (message, tool call, tool result, compaction, or error) and the session is allowlisted, --format sharegpt and --format trajectory follow the existing rules: content_text becomes value; meta, usage, and unknown rows are left out; encrypted_content stays opaque and out of value; ruleset v1 strips plaintext training fields; the CAS object and the normalized JSONL are not rewritten. A fixture with no training turn is named on stderr and omitted
- [ ] go test ./... is green, including a unit or accept-style cursor_cli_store_json fixture. TestUnimplementedHarnessRecordsNormalizeError no longer expects a valid Cursor CLI export to fail

## Definition of done

- [ ] The projector lives in internal/normalize and api.Server.Project calls it for this harness and its artifact kind
- [ ] The harness adapter is unchanged. It still uploads the same bytes and still drops or keeps the same keys
- [ ] The terva projector still projects a terva transcript, and go test ./... passes
