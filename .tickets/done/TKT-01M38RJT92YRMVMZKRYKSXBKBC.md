---
schema: 3
id: TKT-01M38RJT92YRMVMZKRYKSXBKBC
title: Claude Code normalize projector
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
updated_at: 2026-09-24T04:43:15Z
created_by:
  id: agent:cursor/dcb2
  name: Cursor cloud agent
updated_by:
  id: agent:cursor/4360
  name: Cursor cloud agent
extensions: {}
---

## Description

Project a stored Claude Code transcript into schema_version 1 events.

The bytes are the JSONL the adapter already uploads, artifact kind `transcript_jsonl`, harness `claude`. `internal/adapter/claude` reads `type`, `sessionId`, and `cwd`. Every other key stays on `Record.Extra`. `harness_version` is that package's pinned reader version. The file bytes in the CAS are the source. The adapter stays as it is.

`session_id` is `claude:` plus the native session id, the same shape terva uses (`terva:` plus the native id).

Before this ticket, `api.Server.Project` rejected harness `claude` before it read the blob. This ticket extends that branch so a claude manifest uses a Claude projector. `raati_json` and `tasks_json` stay out of the event stream; they are terva sidecars.

### Fixture

Use a Claude JSONL fixture that includes a user or assistant text turn, one unknown key, and an `encrypted_content` value when the line shape can carry one. A line that is not a JSON object fails the blob. The error text does not include the line.

## Acceptance criteria

- [x] A stored Claude Code transcript_jsonl blob projects to schema_version 1 events. session_id is claude: plus the native session id. The CAS object is not opened for write
- [x] Unknown keys on the stored record are kept. encrypted_content, when the fixture carries it, is copied as an opaque value, is left out of content_text, and is not decrypted. The projector does not invent secret or credential values
- [x] A worker given a success fixture of this harness clears normalize_error, writes normalized/<session_uid>.jsonl, and writes a parquet file at parquet/date=YYYY-MM-DD/harness=<harness>/<session_uid>.parquet. The date is the UTC day of recorded_at, or ingested_at when recorded_at is missing. A projection failure sets normalize_error, removes that session's derived JSONL and parquet, and leaves the CAS object in place
- [x] terva-lampi export --format events writes the projected events. When the fixture has a training turn (message, tool call, tool result, compaction, or error) and the session is allowlisted, --format sharegpt and --format trajectory follow the existing rules: content_text becomes value; meta, usage, and unknown rows are left out; encrypted_content stays opaque and out of value; ruleset v1 strips plaintext training fields; the CAS object and the normalized JSONL are not rewritten. A fixture with no training turn is named on stderr and omitted
- [x] go test ./... is green, including a unit or accept-style Claude fixture. TestUnimplementedHarnessRecordsNormalizeError no longer expects harness claude to fail

## Definition of done

- [x] The projector lives in internal/normalize and api.Server.Project calls it for this harness and its artifact kind
- [x] The harness adapter is unchanged. It still uploads the same bytes and still drops or keeps the same keys
- [x] The terva projector still projects a terva transcript, and go test ./... passes

## Implementation plan

### Approach

Add normalize.Claude, a Normalizer for Claude Code transcript_jsonl. It follows normalize.Terva: one JSON object per line, unknown keys in extra, encrypted_content copied opaque and left out of content_text, and a non-object line fails the blob without the line text.

api.Server.Project dispatches harness claude to that projector and reads only transcript_jsonl. raati_json and tasks_json stay out. The claude adapter is not edited. harness_version stays the manifest's pinned reader version. session_id is claude: plus the native session id. parentUuid stays in extra.

### Tests

A unit fixture covers a user text turn, an assistant turn with thinking, tool_use, and a web_search_tool_result that carries encrypted_content, a tool_result, a compact_boundary, an unknown key, an image, and a non-object line. A worker fixture checks the JSONL and parquet paths, a failed projection, and that TestUnimplementedHarnessRecordsNormalizeError no longer lists claude. An export fixture checks events, sharegpt, and trajectory.

## Summary

normalize.Claude projects a stored Claude Code transcript_jsonl blob onto schema_version 1 events. session_id is claude: plus the native session id. api.Server.Project calls it for harness claude and skips raati_json and tasks_json. Unknown keys stay on the event. encrypted_content is copied as stored, left out of content_text, and not decrypted.

A successful worker clears normalize_error and writes normalized/<session_uid>.jsonl plus parquet/date=YYYY-MM-DD/harness=claude/<session_uid>.parquet. A non-object line sets normalize_error, removes that derived view, and leaves the CAS object in place. terva-lampi export --format events writes the projected events. Allowlisted sharegpt and trajectory exports follow the existing training rules. The Claude Code adapter is unchanged. go test ./... passed.
