---
schema: 3
id: TKT-01M38RJT9T26ZGSMXKYJYJYNKQ
title: Codex CLI normalize projector
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

Project a stored Codex CLI rollout into schema_version 1 events.

The bytes are rollout JSONL the adapter already uploads, artifact kind `transcript_jsonl`, harness `codex`. `internal/adapter/codex` reads `timestamp`, `type`, and `payload`. Every other top-level key stays on `Record.Extra`. A `session_meta` payload carries `id` and `cwd`. `history.jsonl` is not a session and is not a rollout. The projector does not treat that file as one. `harness_version` is the adapter's pinned reader version.

`session_id` is `codex:` plus the native session id, the same shape terva uses.

`api.Server.Project` currently rejects harness `codex` before it reads the blob. This ticket extends that branch so a codex manifest uses a Codex projector.

### Fixture

Use a rollout JSONL fixture that includes a user or assistant turn, one unknown key, and an `encrypted_content` value when the line shape can carry one. A line that is not a JSON object fails the blob. The error text does not include the line. `TestUnimplementedHarnessRecordsNormalizeError` does not list harness `codex` today; this ticket adds a success fixture rather than extending that negative list.

## Acceptance criteria

- [ ] A stored Codex CLI rollout transcript_jsonl blob projects to schema_version 1 events. session_id is codex: plus the native session id. history.jsonl is not projected. The CAS object is not opened for write
- [ ] Unknown keys on the stored record are kept. encrypted_content, when the fixture carries it, is copied as an opaque value, is left out of content_text, and is not decrypted. The projector does not invent secret or credential values
- [ ] A worker given a success fixture of this harness clears normalize_error, writes normalized/<session_uid>.jsonl, and writes a parquet file at parquet/date=YYYY-MM-DD/harness=<harness>/<session_uid>.parquet. The date is the UTC day of recorded_at, or ingested_at when recorded_at is missing. A projection failure sets normalize_error, removes that session's derived JSONL and parquet, and leaves the CAS object in place
- [ ] terva-lampi export --format events writes the projected events. When the fixture has a training turn (message, tool call, tool result, compaction, or error) and the session is allowlisted, --format sharegpt and --format trajectory follow the existing rules: content_text becomes value; meta, usage, and unknown rows are left out; encrypted_content stays opaque and out of value; ruleset v1 strips plaintext training fields; the CAS object and the normalized JSONL are not rewritten. A fixture with no training turn is named on stderr and omitted
- [ ] go test ./... is green, including a unit or accept-style Codex rollout fixture

## Definition of done

- [ ] The projector lives in internal/normalize and api.Server.Project calls it for this harness and its artifact kind
- [ ] The harness adapter is unchanged. It still uploads the same bytes and still drops or keeps the same keys
- [ ] The terva projector still projects a terva transcript, and go test ./... passes
