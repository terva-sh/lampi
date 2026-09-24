---
schema: 3
id: TKT-01M3558E3ER1TS33KZB0AG7XX4
title: Version-pinned Cursor state.vscdb snapshot reader
type: task
status: done
status_reason: null
priority: low
due_on: null
labels:
  - area/adapter
  - phase/4-cursor
assignees: []
milestone: phase-4
parent: TKT-01M3558E2PNBKNHFY6J7A37GJ4
origin: null
dependencies:
  - TKT-01M3558DZ6CBW2HB49WFFY20CP
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:12Z
updated_at: 2026-09-23T21:11:41Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/e85b
  name: Cursor cloud agent
extensions: {}
---

## Description

Read-only snapshot of IDE global/workspace DBs. confidence=low. Never ingest cursorAuth/*. Copy WAL trio for consistency.

## Acceptance criteria

- [x] Read-only snapshot of the IDE global state.vscdb and each workspace state.vscdb
- [x] The reader is confidence=low and version-pinned
- [x] Keys under cursorAuth/* are never ingested
- [x] The WAL trio (state.vscdb and present state.vscdb-wal and state.vscdb-shm) is copied for a consistent snapshot before the database is opened
- [x] A major Cursor upgrade may break the pin, and the pin is documented

## Implementation plan

Add a Cursor IDE peer on the same Harness surface as terva, Claude Code, Codex, and OpenCode. Discovery, watch, and sync use that peer. The projector stays unimplemented, so a stored manifest records normalize_error.

### On disk
The user-data directory is the Electron path Cursor inherits from VS Code. On Linux it is $XDG_CONFIG_HOME/Cursor, or ~/.config/Cursor. On macOS it is ~/Library/Application Support/Cursor. On Windows it is %APPDATA%\Cursor, or %USERPROFILE%\AppData\Roaming\Cursor. state.vscdb lives at User/globalStorage/state.vscdb and User/workspaceStorage/<id>/state.vscdb. The workspace directory name is opaque. workspace.json beside a workspace database names the folder.

VSCODE_APPDATA, VSCODE_PORTABLE, and a Windows user-data directory seen from WSL were not confirmed for current Cursor and are not consulted. Other Unix systems follow the Linux XDG path. That was not checked against a Cursor build.

### Snapshot
The live database is never opened and never written. A read copies state.vscdb and, when present, state.vscdb-wal and state.vscdb-shm into a temporary directory, then opens that copy read-only. The copy is removed after the export is built. If the copied shm stops the open, it is removed from the snapshot only and the open is tried once more.

### Export
Version is pinned at 1. Confidence is low, and that string is on the export. ItemTable is required. cursorDiskKV is read when the table exists. Current Cursor builds keep chat bodies there. Keys under cursorAuth/ are dropped, including a different case on that segment. Other keys are kept. JSON values stay JSON. The artifact kind is cursor_state_json. A rewrite replaces the session head.

The global export has an empty cwd, so the allowlist refuses it. One global database holds every workspace, and this reader does not split it. A workspace cwd comes from the folder URI in workspace.json. A vscode-remote URI has no local path, so that workspace stays on the machine.

### What is not read
The Cursor CLI store.db is a different corpus and is not opened. Raw state.vscdb, -wal, and -shm are not the bytes that sync uploads.

### Wiring
agent discover, the long-running watch, and sync ask the harness for its home and its manifests. A missing user-data directory is skipped. The watcher follows globalStorage and workspaceStorage, including the WAL sidecars, so a write that lands in the WAL is noticed. When this adapter landed, normalize workers implemented terva only. Workers now project terva, claude, codex, and opencode. A stored Cursor IDE manifest still records normalize_error.

## Summary

internal/adapter/cursor snapshots Cursor IDE state.vscdb and uploads a filtered JSON export. The user-data directory is $XDG_CONFIG_HOME/Cursor or ~/.config/Cursor on Linux, ~/Library/Application Support/Cursor on macOS, and %APPDATA%\Cursor on Windows. Global storage and each User/workspaceStorage/<id> database are separate. The live files are not opened. The reader copies state.vscdb and any state.vscdb-wal and state.vscdb-shm, opens the copy read-only, and deletes the copy.

Version is pinned at 1. Confidence is low. ItemTable is required. cursorDiskKV is included when it exists. Keys under cursorAuth/ are not in the export. A major Cursor upgrade that renames those tables breaks the pin. agent discover, the watch, and sync use the harness. The global export has no single project cwd, so the allowlist refuses it. A workspace takes its cwd from workspace.json. A vscode-remote URI stays on the machine.

The raw database is not uploaded. The Cursor CLI store.db is not read. Workers project terva, claude, codex, and opencode onto schema_version 1. A stored Cursor IDE manifest still records normalize_error.

go test ./... is green. Landed on cursor/phase4-cursor-state-vscdb-9911 as https://github.com/terva-sh/lampi/pull/25.
