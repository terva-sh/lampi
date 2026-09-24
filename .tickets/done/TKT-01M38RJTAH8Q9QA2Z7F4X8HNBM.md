---
schema: 3
id: TKT-01M38RJTAH8Q9QA2Z7F4X8HNBM
title: OpenCode normalize projector
type: task
status: done
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
updated_at: 2026-09-24T05:57:01Z
created_by:
  id: agent:cursor/dcb2
  name: Cursor cloud agent
updated_by:
  id: agent:cursor/opencode-7843
  name: ""
extensions: {}
---

## Description

Project a stored OpenCode export document into schema_version 1 events.

The intended bytes are one `opencode export` JSON document. The adapter uploads it as artifact kind `transcript_jsonl`, harness `opencode`, even though the file is one JSON document rather than JSONL. `internal/adapter/opencode` reads `info.id` and `info.directory`. Every other key stays on `Record.Extra` or `Info.Extra`. `harness_version` is the adapter's pinned reader version.

When `export/` has no JSON, discovery falls back to a `.db` file at the data-directory root and uploads that file as `transcript_jsonl` with an empty project. That blob is a SQLite database, not an export document. This projector parses the export document. A stored database blob keeps `normalize_error`. The WAL sidecar is not a session.

`session_id` is `opencode:` plus the native session id, the same shape terva uses.

Before this ticket, `api.Server.Project` rejected harness `opencode` before it read the blob. This ticket extends that branch so an opencode export uses an OpenCode projector.

### Fixture

Use an export document that includes a user or assistant turn, one unknown key, and an `encrypted_content` value when the document can carry one. A body that is not a JSON object fails. The error text does not include the body. A `.db` blob is a failure fixture, not a success fixture.

## Acceptance criteria

- [x] A stored opencode export JSON document (artifact kind transcript_jsonl) projects to schema_version 1 events. session_id is opencode: plus the native session id. A .db fallback blob keeps normalize_error. The CAS object is not opened for write
- [x] Unknown keys on the stored record are kept. encrypted_content, when the fixture carries it, is copied as an opaque value, is left out of content_text, and is not decrypted. The projector does not invent secret or credential values
- [x] A worker given a success fixture of this harness clears normalize_error, writes normalized/<session_uid>.jsonl, and writes a parquet file at parquet/date=YYYY-MM-DD/harness=<harness>/<session_uid>.parquet. The date is the UTC day of recorded_at, or ingested_at when recorded_at is missing. A projection failure sets normalize_error, removes that session's derived JSONL and parquet, and leaves the CAS object in place
- [x] terva-lampi export --format events writes the projected events. When the fixture has a training turn (message, tool call, tool result, compaction, or error) and the session is allowlisted, --format sharegpt and --format trajectory follow the existing rules: content_text becomes value; meta, usage, and unknown rows are left out; encrypted_content stays opaque and out of value; ruleset v1 strips plaintext training fields; the CAS object and the normalized JSONL are not rewritten. A fixture with no training turn is named on stderr and omitted
- [x] go test ./... is green, including a unit or accept-style OpenCode export fixture. TestUnimplementedHarnessRecordsNormalizeError no longer expects a valid OpenCode export to fail

## Definition of done

- [x] The projector lives in internal/normalize and api.Server.Project calls it for this harness and its artifact kind
- [x] The harness adapter is unchanged. It still uploads the same bytes and still drops or keeps the same keys
- [x] The terva projector still projects a terva transcript, and go test ./... passes

## Implementation plan

### Approach

Add normalize.OpenCode, a Normalizer for one opencode export JSON document stored as transcript_jsonl. It follows normalize.Claude and normalize.Codex: harness-local projection onto schema_version 1, unknown keys in extra, encrypted_content copied opaque and left out of content_text. A body that is not one JSON object, including a SQLite database blob and trailing junk, fails the blob. The error text does not include the body. The raw bytes are not written.

api.Server.Project calls it for harness opencode and reads transcript_jsonl only. The adapter, CAS, and queue stay as they are. schema_version stays 1.

Session info becomes a meta event. Message parts become message, tool_call, tool_result, usage, compaction, error, or unknown events. session_id is opencode: plus the native id. info.parentID is the parent session. A message parentID stays in extra. harness_version stays the caller's pinned reader version. OpenCode timestamps are unix milliseconds.

### Tests

A unit fixture covers a user text turn, reasoning that carries encrypted_content, a tool call and result, a file data URL, a compaction, usage, an error, an unknown key, a non-object body, and a SQLite blob. A worker fixture checks the JSONL and parquet paths, a failed projection, and a database blob. An export fixture checks events, sharegpt, and trajectory. TestUnimplementedHarnessRecordsNormalizeError no longer lists opencode.

## Summary

normalize.OpenCode projects a stored OpenCode export JSON document onto schema_version 1 events. The blob is artifact kind transcript_jsonl. session_id is opencode: plus the native session id. api.Server.Project calls it for harness opencode and reads transcript_jsonl only. Unknown keys stay on the event. encrypted_content is copied as stored, left out of content_text, and not decrypted. harness_version stays the adapter's pinned reader version. File and data-URL bytes stay in the raw blob.

A SQLite database blob, or any body that is not one JSON object, sets normalize_error. The error text does not include the body. The derived JSONL and parquet are removed, and the CAS object stays in place.

A successful worker clears normalize_error and writes normalized/<session_uid>.jsonl plus parquet/date=YYYY-MM-DD/harness=opencode/<session_uid>.parquet. The date is the UTC day of recorded_at, or ingested_at when the record has no time. terva-lampi export --format events writes the projected events. Allowlisted sharegpt and trajectory exports follow the existing training rules. The OpenCode adapter is unchanged. schema_version stays 1. go test ./... passed.
