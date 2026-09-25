---
schema: 3
id: TKT-01M38RJTB89GY6GWC3XZZFYYY9
title: Cursor IDE normalize projector
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
updated_at: 2026-09-25T02:19:57Z
created_by:
  id: agent:cursor/dcb2
  name: Cursor cloud agent
updated_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
extensions: {}
---

## Description

Project a stored Cursor IDE export into schema_version 1 events.

The bytes are the filtered JSON the adapter already uploads, artifact kind `cursor_state_json`, harness `cursor`. The document carries `harness_version`, `confidence`, `source`, `scope`, `item_table`, and, when the snapshot had the table, `cursor_disk_kv`. The raw `state.vscdb` is not in the CAS. Reader version is 1 and confidence is low: unknown keys stay on the event, and a key this projector does not interpret is kept rather than dropped.

Keys under `cursorAuth/` are already absent from the export. The projector leaves them absent. It does not invent credential values.

`session_id` is `cursor:` plus the native session id, the same shape terva uses.

`api.Server.Project` dispatches terva, claude, codex, opencode, and cursor. cursor reads `cursor_state_json`. It rejects `cursor-cli` before it reads a blob. terva reads `transcript_jsonl` and `errors_jsonl`. claude, codex, and opencode read `transcript_jsonl` only. A cursor manifest with no `cursor_state_json` artifact is a failure: the worker sets `normalize_error` and writes no derived files. This projector does not read `cursor_cli_store_json`.

### Fixture

Use a `cursor_state_json` document with a conversation row, one unknown key, and an `encrypted_content` value when a row can carry one. A `transcript_jsonl` blob on a cursor session is a failure fixture.

## Acceptance criteria

- [x] A stored cursor_state_json export projects to schema_version 1 events. session_id is cursor: plus the native session id. A transcript_jsonl blob on a cursor session is a failure. The CAS object is not opened for write
- [x] Unknown keys on the stored record are kept. encrypted_content, when the fixture carries it, is copied as an opaque value, is left out of content_text, and is not decrypted. The projector does not invent secret or credential values
- [x] A worker given a success fixture of this harness clears normalize_error, writes normalized/<session_uid>.jsonl, and writes a parquet file at parquet/date=YYYY-MM-DD/harness=<harness>/<session_uid>.parquet. The date is the UTC day of recorded_at, or ingested_at when recorded_at is missing. A projection failure sets normalize_error, removes that session's derived JSONL and parquet, and leaves the CAS object in place
- [x] terva-lampi export --format events writes the projected events. When the fixture has a training turn (message, tool call, tool result, compaction, or error) and the session is allowlisted, --format sharegpt and --format trajectory follow the existing rules: content_text becomes value; meta, usage, and unknown rows are left out; encrypted_content stays opaque and out of value; ruleset v1 strips plaintext training fields; the CAS object and the normalized JSONL are not rewritten. A fixture with no training turn is named on stderr and omitted
- [x] go test ./... is green, including a unit or accept-style cursor_state_json fixture. TestUnimplementedHarnessRecordsNormalizeError no longer expects a valid Cursor IDE export to fail

## Definition of done

- [x] The projector lives in internal/normalize and api.Server.Project calls it for this harness and its artifact kind
- [x] The harness adapter is unchanged. It still uploads the same bytes and still drops or keeps the same keys
- [x] The terva projector still projects a terva transcript, and go test ./... passes

## Notes

**agent:claude-code/eh1m** at 2026-09-25T02:19:17Z

Verified against main at 2a0726c. The projector landed as PR #39 (6d7b567). Every acceptance criterion and definition-of-done item holds.

### Evidence

- AC1: `normalize.Cursor` in internal/normalize/cursor.go. TestCursorSchemaOpaqueAndUnknownKeys checks schema_version 1 and that the raw bytes are unchanged. TestCursorSessionIDPrefersNative checks `cursor:` plus the native id. TestCursorTranscriptJSONLRejected, TestCursorWorkerTranscriptJSONLSetsError and TestCursorKindSkipIsNormalizeError cover the transcript_jsonl failure. The api tests compare the CAS bytes after the worker runs.
- AC2: TestCursorSchemaOpaqueAndUnknownKeys keeps unknown keys, the top-level unknown `cli_version`, and `encrypted_content` on extra and out of content_text. It also checks that a base64 wrapper is not decoded. cursor.go has no decrypt path. TestCursorAuthAbsent covers the clean and hostile cursorAuth cases.
- AC3: TestCursorWorkerProjectsStateJSON checks a cleared normalize_error, normalized/<uid>.jsonl, the parquet path from PartitionDate and `harness=cursor`, and the recorded_at day. TestCursorWorkerCorruptHeadDropsDerived checks that a failure sets normalize_error, removes the JSONL and parquet, and leaves both CAS objects in place.
- AC4: TestCursorExportEventsShareGPTAndTrajectory in internal/cli/export_cursor_test.go. It covers events, sharegpt and trajectory output, the ruleset v1 placeholder, opaque encrypted_content, meta and unknown rows left out, a no-turn session named on stderr, and the CAS object and normalized JSONL left unrewritten.
- AC5: go test ./... passes, including go test -race. TestUnimplementedHarnessRecordsNormalizeError in internal/api/worker_test.go now checks that a valid cursor_state_json export projects and clears normalize_error. It still expects a transcript_jsonl blob on a cursor session to fail.
- DoD1: api.Server.Project in internal/api/project.go dispatches HarnessCursor to normalize.Cursor, and projectKind limits it to cursor_state_json.
- DoD2: PR #39 changed no file under internal/adapter or internal/upload. It touched api, cli/export.go, normalize, and a doc comment in protocol.go.
- DoD3: TestTervaSchemaAndOpaqueFields and the rest of internal/normalize pass.

### Later work, not a failure

The description says Project rejects `cursor-cli` and that the reader version is 1. Later tickets changed both. PR #41 (f75d678) added the Cursor CLI projector, so Project now dispatches cursor-cli to normalize.CursorCLI. PR #53 (795349c) changed the Cursor adapter for workspace enrichment and moved the reader version to 2. Neither change undoes this ticket.

## Summary

Landed as PR #39 (6d7b567). normalize.Cursor projects cursor_state_json onto schema_version 1, and api.Server.Project dispatches harness cursor to it. Verified on main at 2a0726c; the evidence is in the latest note.
