---
schema: 3
id: TKT-01M3558DZXWZWN1SEFHN2SF6S4
title: OpenCode via scheduled opencode export
type: task
status: done
status_reason: null
priority: low
due_on: null
labels:
  - area/adapter
  - phase/3-export
assignees: []
milestone: phase-3
parent: TKT-01M3558DZ6CBW2HB49WFFY20CP
origin: null
dependencies:
  - TKT-01M3558DV5NHYZBFMPV1AVRNYQ
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:12Z
updated_at: 2026-09-23T17:52:57Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/368c
  name: Cursor cloud agent
extensions: {}
---

## Description

Prefer `opencode export` / db path discovery over live SQLite WAL tails.

## Acceptance criteria

- [x] Ingest prefers a scheduled `opencode export` or a discovered database path
- [x] Live SQLite WAL tails are not the read path

## Implementation plan

Add an OpenCode peer on the same Harness surface as terva, Claude Code, and Codex. Discovery, watch, and sync use that peer. When this adapter landed, the projector was unimplemented, so a stored manifest recorded normalize_error. normalize.OpenCode now projects an export document onto schema_version 1. A stored database blob still keeps normalize_error.

### On disk
OpenCode's data directory is `$XDG_DATA_HOME/opencode`, or `~/.local/share/opencode` when that variable is unset (`%USERPROFILE%\.local\share\opencode` on Windows). That is the xdg-basedir path OpenCode uses. There is no `OPENCODE_HOME`.

`opencode export [sessionID]` writes one JSON document to stdout, `{info, messages}`, and does not write a file of its own. A schedule redirects that into `export/**/*.json` under the data directory. `info.id` is the session id and `info.directory` is the project cwd the allowlist already matches. The watcher follows `export/`, the same way Claude follows `projects/` and Codex follows `sessions/`.

### What is not read
The live database is SQLite WAL. `*.db-wal`, `*.db-shm`, and `*.db-journal` never match. The adapter does not open the database, so it does not follow the WAL. When `export/` has no JSON, discovery falls back to a `*.db` file at the data-directory root (`opencode.db`, a channel file such as `opencode-stable.db`, or the relative name `OPENCODE_DB` places there). That file is one raw blob. It has no session directory on the outside, so the allowlist refuses it. An export, which names one directory, is the path that can leave the machine. If any export JSON exists, the database file is not in the discover set.

### Record shape
Version is pinned at 1 inside the adapter. The export document stays internal. Keys the reader does not interpret are kept and survive a re-encode. `harness_version` is that reader version. Sync uploads the file bytes.

### Wiring
`agent discover`, the long-running watch, and `sync` ask the harness for its home and its manifests. A missing data directory is skipped, as with Claude and Codex. When this adapter landed, normalize workers implemented terva only. normalize.OpenCode now projects an export document onto schema_version 1. A stored database blob still keeps `normalize_error`.

## Summary

internal/adapter/opencode discovers and watches a scheduled `opencode export` under the OpenCode data directory's `export/**/*.json`. That directory is `$XDG_DATA_HOME/opencode`, or `~/.local/share/opencode` when the variable is unset (`%USERPROFILE%\.local\share\opencode` on Windows). `opencode export` writes one JSON document to stdout; a schedule redirects it into that directory. `info.id` is the session id and `info.directory` is the cwd the allowlist matches.

When `export/` has no JSON, discovery uses a `*.db` file at the data-directory root (`opencode.db`, a channel file such as `opencode-stable.db`, or another name `OPENCODE_DB` places there). That file is not watched as it grows. `opencode.db-wal`, `opencode.db-shm`, and `opencode.db-journal` are never listed, and the adapter does not open the database.

Version is pinned at 1. Keys the reader does not interpret stay on the record and survive a re-encode. `harness_version` is that reader version. `agent discover`, the watch, and `sync` use the harness. A database blob has no session directory, so the allowlist refuses it: one file holds every project. An export is the path that can leave the machine.

When this adapter landed, normalize workers implemented terva only. normalize.OpenCode now projects an export document onto schema_version 1. A stored database blob still keeps `normalize_error`.
