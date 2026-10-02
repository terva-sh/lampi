---
schema: 3
id: TKT-01M3Z02QJNXSYB4XQHPJGD9R59
title: Adapter discover/watch/manifest for Grok Build
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/adapter
  - phase/2-harness
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references:
  - ref: pr:79
    path: null
claim:
  actor: agent:cursor/6699
  branch: cursor/grok-build-adapter-6699
  worktree: /workspace
  commit: 4ba8d69b23f514371d625e8ce2eac35b0dbe70ca
  session: null
  claimed_at: 2026-10-02T19:05:18Z
  expires_at: null
archive: null
created_at: 2026-10-02T19:04:57Z
updated_at: 2026-10-02T19:23:41Z
created_by:
  id: agent:cursor/6699
  name: Cursor cloud agent
updated_by:
  id: agent:cursor/6699
  name: Cursor cloud agent
extensions: {}
---

## Description

Grok Build is harness id `grok`. This ticket is discover, watch, and manifests. It does not project events.

### On disk
Home is `GROK_HOME`, or `~/.grok` when that is unset. A session is `sessions/<encoded-cwd>/<uuid>/updates.jsonl`. The directory name is the URL-encoding of the cwd. When that encoding is longer than 255 bytes, the directory is a slug plus the first 16 hex characters of BLAKE3(cwd), and a `.cwd` file in the group directory holds the original path. `summary.json` beside `updates.jsonl` carries `info.cwd`, the title (`generated_title`, otherwise `session_summary`), and `current_model_id`.

### Upload
`updates.jsonl` is the transcript (`transcript_jsonl`) and the source of truth. `summary.json` is a `summary_json` companion on the same manifest. The native session id is the session UUID from the path. `harness_version` is `1`. The JSONL reader keeps unknown keys and has no Confidence field. `chat_history.jsonl` and the other session files are not uploaded.

### Out of this ticket
Normalize, which will name events `grok:<uuid>`, is a separate ticket. Soft-link, AgentsView, deja-vu, the Cursor adapters, Grok Bot server chats, full sidecars, usage promotion, ship and user-unit install, and operator docs are out.

## Acceptance criteria

- [x] Discover and watch report sessions/<encoded-cwd>/<uuid>/updates.jsonl as transcript_jsonl and the sibling summary.json as summary_json
- [x] Native session id is the session UUID, and catalog Current(grok, uuid) finds the session after upload
- [x] summary.json supplies cwd, title, and model, including a cwd whose encoded dirname exceeds 255 bytes
- [x] PlantGrok writes that layout; a relative cwd, a non-UUID id, or an unknown path does not panic, and unknown files do not fail Discover or Manifests
- [x] Cursor IDE and Cursor CLI Match and Confidence are unchanged
- [x] harness_version is 1 and the JSONL record has no Confidence field
- [x] Normalize is not implemented; a stored grok manifest records normalize_error

## Definition of done

- [x] grok is registered for discover, watch, upload, and catalog ingest
- [x] The acceptance tests pass
- [x] Cursor adapter packages are not edited

## Implementation plan

Follow the Claude JSONL adapter for layout and registration. Add `internal/adapter/grok`, register harness `grok` on discover, watch, upload, and catalog ingest, and add `PlantGrok`.

### Reader
`Match` accepts `sessions/<encoded-cwd>/<uuid>/updates.jsonl` as `transcript_jsonl` and the sibling `summary.json` as `summary_json`. Anything else under the home is ignored. The native id is the UUID path segment. Project cwd comes from `summary.json` `info.cwd`, then from URL-decoding the group directory, then from the group `.cwd` file. Title and model are read from the summary and the raw summary bytes are the companion artifact. `harness_version` stays `1`. The line record has no Confidence field.

### Not in this change
No normalize projector. A stored grok manifest records `normalize_error`. Cursor and Cursor CLI packages stay as they are.

## Notes

**agent:cursor/6699** at 2026-10-02T19:22:25Z

Discover, watch, and upload for harness `grok` are on `cursor/grok-build-adapter-6699`. Normalize stays unimplemented. A stored grok manifest records `normalize_error` containing "not implemented".

### Cut notes
A session id that is not a UUID is not matched, so a `grok -s` client id is not a session here. A relative `GROK_HOME` is used as given and is not resolved from the process cwd. `chat_history.jsonl` and the other session files are not uploaded. `summary.json` is watched, so a rewrite of the companion is seen, and it does not move the transcript head. Event ids `grok:<uuid>` belong to the normalize ticket.

**agent:cursor/6699** at 2026-10-02T19:23:41Z

Opened https://github.com/terva-sh/lampi/pull/79 for this ticket. The pull request is ready for review. Squash-merge waits on review.
