---
schema: 3
id: TKT-01M38RJCDREDTTPTY2D7SR8W59
title: Normalize projectors for non-terva harnesses
type: epic
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/normalize
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-24T03:50:24Z
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

Extend normalize workers so an ingested non-terva manifest projects onto the same schema_version 1 events, JSONL, and parquet path as terva. A Claude Code, Codex CLI, OpenCode, Cursor IDE, or Cursor CLI manifest is already stored. The worker sets normalize_error because that harness projector is missing.

Discovery, watch, and upload stay as they are. The adapters already exist. This epic is the derived view. It leaves the closed Phase 2, Phase 3, Phase 4, and Phase 5 epics closed. Those closed when the bytes were stored.

### Harnesses

One child per harness. Cursor IDE and Cursor CLI are separate children. The stored documents differ. The IDE export is `cursor_state_json`: ItemTable and an optional cursorDiskKV, from a `state.vscdb` snapshot, harness `cursor`. The CLI export is `cursor_cli_store_json`: meta and blobs, from a `store.db` snapshot, harness `cursor-cli`. The two do not share sessions, watermarks, or a document schema. One projector cannot cover both.

### Dispatch

`normalize.Normalizer` already exists. `Terva` implements it. `api.Server.Project` is the terva-only branch: it rejects every other harness before it reads a blob, and it only reads `transcript_jsonl` and `errors_jsonl`. Claude Code, Codex CLI, and OpenCode already store `transcript_jsonl`. Cursor IDE and Cursor CLI store the kinds above. Each child extends that branch for its harness and its artifact kind. No child waits on another. `normalize.Normalizer` is already the seam, so there is no shared interface ticket in front of the harness work.

### Architecture note

The documentation update is an acceptance criterion of this epic. It is not its own ticket, and it is not owned by one harness child. When the children are done, `docs/architecture.md` should stop saying workers implement terva only. That is the Normalize row in "What this tree does", the `internal/normalize` row in "What this tree does not do", and the paragraph that says a worker sets `normalize_error` when the projector is missing. Land that edit with the last child, or as the close of this epic.

## Acceptance criteria

- [ ] Each child projects its harness onto schema_version 1 events, JSONL, and parquet, and a fixture of that harness no longer keeps a permanent normalize_error
- [ ] When the children are done, docs/architecture.md names Claude Code, Codex CLI, OpenCode, Cursor IDE, and Cursor CLI as normalize projectors on the schema_version 1 path. The Normalize row, the internal/normalize row under What this tree does not do, and the paragraph that currently says a missing projector sets normalize_error all get that edit. The edit lands with the last child or as the close of this epic

## Definition of done

- [ ] All children of this epic are done
- [ ] TKT-01M38RJT92YRMVMZKRYKSXBKBC Claude Code normalize projector
- [ ] TKT-01M38RJT9T26ZGSMXKYJYJYNKQ Codex CLI normalize projector
- [ ] TKT-01M38RJTAH8Q9QA2Z7F4X8HNBM OpenCode normalize projector
- [ ] TKT-01M38RJTB89GY6GWC3XZZFYYY9 Cursor IDE normalize projector
- [ ] TKT-01M38RJTC2C4KAGM3SK7ABE1WA Cursor CLI normalize projector
- [ ] docs/architecture.md no longer says normalize implements terva only
