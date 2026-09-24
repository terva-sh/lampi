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
updated_at: 2026-09-24T07:10:25Z
created_by:
  id: agent:cursor/dcb2
  name: Cursor cloud agent
updated_by:
  id: agent:cursor/56e9
  name: Cursor cloud agent
extensions: {}
---

## Description

Extend normalize workers so an ingested non-terva manifest projects onto the same schema_version 1 events, JSONL, and parquet path as terva. Workers already project terva, claude, codex, opencode, and cursor. A Cursor CLI manifest is stored, and the worker sets normalize_error because that harness projector is missing.

Discovery, watch, and upload stay as they are. The adapters already exist. This epic is the derived view. It leaves the closed Phase 2, Phase 3, Phase 4, and Phase 5 epics closed. Those closed when the bytes were stored.

### Harnesses

One child per harness. Cursor IDE and Cursor CLI are separate children. The stored documents differ. The IDE export is `cursor_state_json`: ItemTable and an optional cursorDiskKV, from a `state.vscdb` snapshot, harness `cursor`. The CLI export is `cursor_cli_store_json`: meta and blobs, from a `store.db` snapshot, harness `cursor-cli`. The two do not share sessions, watermarks, or a document schema. One projector cannot cover both.

### Dispatch

`normalize.Normalizer` already exists. `Terva`, `Claude`, `Codex`, `OpenCode`, and `Cursor` implement it. `api.Server.Project` dispatches those five. It reads `transcript_jsonl` and `errors_jsonl` for terva, `transcript_jsonl` only for claude, codex, and opencode, and `cursor_state_json` for cursor. It rejects `cursor-cli` before it reads a blob. Cursor CLI stores `cursor_cli_store_json`. The remaining child extends that branch for harness `cursor-cli` and that artifact kind. `normalize.Normalizer` is already the seam, so there is no shared interface ticket in front of the harness work.

### Architecture note

The documentation update is an acceptance criterion of this epic. It is not its own ticket, and it is not owned by one harness child. A mid-epic edit of `docs/architecture.md` names terva, Claude Code, Codex CLI, OpenCode, and Cursor IDE as projectors on the schema_version 1 path. Cursor CLI (`cursor-cli` / `cursor_cli_store_json`) still records `normalize_error`. That edit covers the Normalize row, the `internal/normalize` row under "What this tree does not do", and the Flow paragraph that used to say workers implement terva only. Naming Cursor CLI as a projector, and retiring the remaining terva-only line, lands with the last child, Cursor CLI, or as the close of this epic.

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

## Notes

**agent:cursor/56e9** at 2026-09-24T06:33:25Z

Mid-epic honesty edit. docs/architecture.md now says workers project terva, claude, codex, and opencode onto schema_version 1. Cursor IDE (`cursor` / `cursor_state_json`) and Cursor CLI (`cursor-cli`) still record normalize_error.

The acceptance criteria and the definition of done are unchanged. Naming Cursor IDE and Cursor CLI as projectors, and retiring the remaining terva-only line, still waits on the last child, TKT-01M38RJTC2C4KAGM3SK7ABE1WA (Cursor CLI normalize projector). TKT-01M38RJTB89GY6GWC3XZZFYYY9 (Cursor IDE normalize projector) is the other open child. Neither status nor those checkboxes moved.

**agent:cursor/56e9** at 2026-09-24T07:10:25Z

This note supersedes the note at 2026-09-24T06:33:25Z. Cursor IDE normalize landed on main as the squash-merge of PR #39 (6d7b567). Workers project terva, claude, codex, opencode, and cursor onto schema_version 1. Only Cursor CLI (`cursor-cli` / `cursor_cli_store_json`) still records normalize_error.

The acceptance criteria and the definition of done are unchanged. Naming Cursor CLI as a projector, and retiring the remaining terva-only line, still waits on TKT-01M38RJTC2C4KAGM3SK7ABE1WA (Cursor CLI normalize projector). Status and checkboxes did not move.
