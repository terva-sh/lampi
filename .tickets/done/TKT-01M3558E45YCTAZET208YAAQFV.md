---
schema: 3
id: TKT-01M3558E45YCTAZET208YAAQFV
title: Cursor CLI store.db adapter (separate corpus)
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
  - TKT-01M3558E3ER1TS33KZB0AG7XX4
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:12Z
updated_at: 2026-09-23T21:58:29Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/ac2a
  name: Cursor cloud agent
extensions: {}
---

## Description

Separate from IDE corpus; do not assume sync.

## Acceptance criteria

- [x] The CLI store.db corpus is separate from the IDE state.vscdb corpus
- [x] The adapter does not assume the CLI store is in sync with IDE state
- [x] This ticket depends on the IDE state.vscdb reader

## Implementation plan

Add a Cursor CLI peer beside the IDE reader. The package is internal/adapter/cursorcli. The harness name is cursor-cli. The artifact kind is cursor_cli_store_json. Discover, watch, and sync register it the same way as the other optional harnesses. The IDE adapter stays on harness cursor and is not changed to open store.db.

### On disk
The config directory is the one Cursor documents for cli-config.json. CURSOR_CONFIG_DIR replaces it. On Linux and other Unix systems except macOS, $XDG_CONFIG_HOME/cursor is used when that variable is set. The default is ~/.cursor on macOS and Linux, and %USERPROFILE%\.cursor on Windows. Chats are chats/<workspace-hash>/<session-uuid>/store.db under that directory. The hash is not turned into a path. A sibling meta.json cwd is used only when it is an absolute path. ACP sessions, projects/*/agent-transcripts JSONL, and auth.json are not read.

The chats layout, the blobs and meta tables, hex-encoded meta JSON, and WAL sidecars are taken from on-disk descriptions, not from Cursor's docs. Windows chats/ was not checked against a Cursor CLI build. The Windows home is still the documented config directory.

### Snapshot
The live database is never opened and never written. A read copies store.db and any store.db-wal and store.db-shm, opens the copy read-only, and deletes the copy. Version is 1. Confidence is low. blobs and meta are required. Keys under cursorAuth/ are dropped, including a different case, and the same rule drops JSON object keys. Exact credential field names (accessToken, refreshToken, and the underscore forms, plus idToken, sessionToken, and workosCursorSessionToken) are dropped the same way. Other tables, protobuf, and auth.json are not read.

### Separation
Watermarks and catalog sessions include the harness name. A CLI export is not a cursor session, and an IDE export is not a cursor-cli session. An empty cwd is refused by the allowlist. Normalize still implements terva only, so a stored manifest records normalize_error.

## Summary

internal/adapter/cursorcli snapshots Cursor CLI store.db and uploads a filtered JSON export. The harness is cursor-cli. It does not read state.vscdb, and the IDE reader does not read store.db. Catalog sessions and watermarks include the harness, so the two corpora do not share sync state.

The config directory is the one Cursor documents for cli-config.json. CURSOR_CONFIG_DIR replaces it. On Linux and other Unix systems except macOS, $XDG_CONFIG_HOME/cursor is used when that variable is set. The default is ~/.cursor on macOS and Linux, and the .cursor directory under USERPROFILE on Windows. Chats are chats/<workspace-hash>/<session-uuid>/store.db. That layout, the blobs and meta tables, hex-encoded meta JSON, and WAL sidecars are not in Cursor's docs. They are what on-disk readers report for Linux, WSL, and macOS. Windows chats/ was not checked against a Cursor CLI build. ACP sessions and projects/*/agent-transcripts JSONL are not read. auth.json is not opened.

The live database is not opened. The reader copies store.db and any store.db-wal and store.db-shm, opens the copy read-only, and deletes the copy. Version is 1. Confidence is low. Keys under cursorAuth/ are dropped, and so are credential field names such as accessToken. An absolute cwd in the sibling meta.json is the project path. Without one, the allowlist refuses the export. The workspace hash is not a path.

agent discover, the watch, and sync use the harness. Normalize workers still implement terva only. A stored Cursor CLI manifest records normalize_error.

go test ./... is green. Landed on cursor/cursor-cli-store-db-ac2a.
