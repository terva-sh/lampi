---
schema: 3
id: TKT-01M3558DVTRM3ZFEYXNH191KES
title: Claude Code JSONL adapter
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/adapter
  - phase/2-harness
assignees: []
milestone: phase-2
parent: TKT-01M3558DV5NHYZBFMPV1AVRNYQ
origin: null
dependencies:
  - TKT-01M3558DPVCN661600P3F9HMB0
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:11Z
updated_at: 2026-09-23T13:01:52Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/7a25
  name: Cursor cloud agent
extensions: {}
---

## Description

Discover/watch $CLAUDE_CONFIG_DIR/projects/**/*.jsonl. Treat record shape as internal; pin adapter version; preserve unknown fields.

## Acceptance criteria

- [x] Discovers and watches $CLAUDE_CONFIG_DIR/projects/**/*.jsonl
- [x] Adapter version is pinned and the record shape is treated as internal
- [x] Unknown fields are preserved

## Implementation plan

Extend adapter.Harness with Home, WatchDir, Match, and Manifests so a peer is discovered, watched, and synced without a one-off path in the agent. Claude Code is the second implementation, beside terva.

### On disk
CLAUDE_CONFIG_DIR wins. When it is unset the directory is ~/.claude (USERPROFILE\.claude on Windows). That is Claude Code's documented default, resolved the same way terva resolves TERVA_HOME: the variable, then the default. There is no XDG fallback. Discovery walks projects/**/*.jsonl. A missing projects directory is an empty list.

### Record shape
Version is pinned at 1 inside the adapter. The JSON object on each line stays internal to that package and is not added to capture protocol 1 or the README. The reader copies keys it does not interpret onto Record.Extra as raw JSON. harness_version on the manifest is that pinned reader version. The raw file bytes are what sync uploads.

### Wiring
agent discover, the long-running watch, and sync all ask the harness for its home and its manifests. The allowlist is unchanged: cwd, cwd hash, and origin's git remote, still default deny. When this adapter landed, the synchronous normalizer projected only terva, and a stored Claude manifest recorded normalize_error. normalize.Claude now projects that transcript onto schema_version 1.

## Summary

internal/adapter/claude discovers and watches $CLAUDE_CONFIG_DIR/projects/**/*.jsonl. When CLAUDE_CONFIG_DIR is unset the directory is ~/.claude (USERPROFILE\.claude on Windows), the default Claude Code documents, resolved the same way terva resolves TERVA_HOME: the variable, then the default, with no XDG fallback.

Version is pinned at 1. The JSON object on each line stays inside that package. Keys the reader does not interpret are kept on Record.Extra and survive a re-encode. harness_version on the manifest is that reader version, not the Claude Code version string in the file. agent discover, the long-running watch, and sync all use the harness. The allowlist is unchanged. When this adapter landed, the synchronous projector implemented terva only. normalize.Claude now projects a stored Claude transcript onto schema_version 1.

Tests cover the glob, a missing projects directory, the pinned version, unknown fields, and an fsnotify and poll watch that reports an append offset.
