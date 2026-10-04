---
schema: 3
id: TKT-01M44B45JRVQ1W2XF5H0K3MC4X
title: "Cursor CLI: read ACP sessions under acp-sessions/"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/adapter
  - area/agent
  - phase/4-cursor
assignees: []
milestone: null
parent: null
origin: null
dependencies:
  - TKT-01M44B45GTE4ZFV6Y86P9A7RKE
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-04T20:54:11Z
updated_at: 2026-10-04T20:54:17Z
created_by:
  id: agent:claude-code/580cbe08
  name: ""
updated_by:
  id: agent:claude-code/580cbe08
  name: ""
extensions: {}
---

## Description

Cursor sessions that an ACP client starts (T3 Code drives the Cursor agent over the Agent Client Protocol) are written to `acp-sessions/<session-uuid>/store.db` under the Cursor CLI config directory, beside a `meta.json` that carries an absolute `cwd`. The `cursor-cli` reader walks only `chats/<workspace>/<session>/store.db` and says in its package comment that ACP sessions are not read.

On the workstation where this was found (2026-10-04), every Cursor session since 2026-09-22 is an ACP session. There were 9 with a database, the newest from that day, and 862 directories that hold only a `meta.json` with no database. The three `chats/` sessions have no `meta.json`, so the allowlist refuses them, and nothing from Cursor reached the lake.

The ACP `store.db` has the same `blobs` and `meta` tables, and the meta value under key `0` is the same hex-encoded JSON record. The export, the redaction scan and the normalize projector apply as they are. What differs is the path (no workspace level), the session id, and the size: these databases run from 244K to 2.5G.

Uploading is a whole-document re-export on every change (see the per-blob ticket), so an active ACP session would be re-snapshotted and re-uploaded on every debounce. Until per-blob upload lands, the reader should not upload a session while it is still being written.

## Acceptance criteria

- [ ] discover, watch and sync read acp-sessions/<uuid>/store.db under the Cursor CLI config directory
- [ ] An ACP session id cannot collide with a chats/ session id, and normalize projects both
- [ ] A directory with only meta.json is skipped without a refuse line
- [ ] An ACP session's cwd comes from its sibling meta.json and passes the same allowlist
- [ ] An ACP session is not uploaded while its database is still being written
- [ ] docs/harnesses.md and the agent help describe the ACP layout
