---
schema: 3
id: TKT-01M44B45JRVQ1W2XF5H0K3MC4X
title: "Cursor CLI: read ACP sessions under acp-sessions/"
type: task
status: in-progress
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
  - TKT-01M44BBWNGHDGG8SSK1WTSWGDD
blocks_on: none
references: []
claim:
  actor: agent:claude-code/580cbe08
  branch: cursor/acp-sessions
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-580cbe08
  commit: c5503b4f52daa8a742878ad526d18f67eed14670
  session: null
  claimed_at: 2026-10-04T21:12:43Z
  expires_at: null
archive: null
created_at: 2026-10-04T20:54:11Z
updated_at: 2026-10-04T21:12:43Z
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

- [x] discover, watch and sync read acp-sessions/<uuid>/store.db under the Cursor CLI config directory
- [x] An ACP session id cannot collide with a chats/ session id, and normalize projects both
- [x] A directory with only meta.json is skipped without a refuse line
- [x] An ACP session's cwd comes from its sibling meta.json and passes the same allowlist
- [x] An ACP session is not uploaded while its database is still being written
- [x] docs/harnesses.md and the agent help describe the ACP layout

## Implementation plan

- **Layout.** `storePath` accepts `acp-sessions/<session>/store.db` and its sidecars beside `chats/<ws>/<session>/store.db`. `Discover` walks both trees. The adapter implements `WatchRoots` so the agent watches both. A directory with only `meta.json` has no `store.db`, so it is never listed and never refused.
- **Session id.** `acp-sessions/<session>`, the export directory, the same rule as chats. The prefix keeps a chat and an ACP session with the same uuid apart. Normalize does not parse the id, so it projects both as they are.
- **Cwd.** The existing sibling `meta.json` read; ACP sessions write an absolute cwd there.
- **Hold.** `ManifestsMemo` takes a settle duration. A permitted session whose `store.db` or WAL mtime is within it goes on `Bundle.Held` (no digest, counted in the inventory) with `Bundle.HeldUntil`. `upload.Result` carries `Held` and `HeldUntil`. The agent sets `Options.Settle` to `cursorcli.SettleAfter` (5 minutes) and arms a timer to run a pass at `HeldUntil`. A one-shot `sync` leaves it zero and uploads what is there. This applies to chats too: they have the same whole-document cost.
- **Size cap.** A store over 256 MiB (store.db plus WAL) is skipped with a line naming it and its size. Added after measuring a real export (see note). TKT-01M44B45MT89CGR6M0HHTWG63P removes the reason for it.

Alternatives considered:

- A per-harness watcher debounce instead of a hold in the reader. Lost: any harness's write starts a pass that rebuilds every bundle, so a watcher-only delay would not stop a Claude write from exporting an active Cursor session.
- Hold in `sync` as well. Lost: a person who runs `sync` asked for an upload now, and the existing tests that write a store and sync at once describe that contract.
- Leave held sessions out of the bundle entirely. Lost: the inventory would drop the session's project while it was being written and add it back after, which reads as a change on the lake's dashboard.
- 2 or 10 minutes for the settle time. 5 minutes is long enough to cover a pause between agent turns, so one conversation is not uploaded several times, and short enough that a session reaches recall soon after it stops.
- No size cap until per-blob upload. Lost: the measured peak memory for a 359 MiB store makes a 2.5 GiB store a likely out-of-memory kill of the agent.

## Notes

**agent:claude-code/580cbe08** at 2026-10-04T21:12:43Z

Checked against this workstation's real ~/.cursor with an isolated XDG config and state (the live agent untouched). agent discover lists the 9 ACP stores and the 3 chats and none of the 862 meta.json-only directories. A real export of the 244K ACP store: 47,647 bytes in 5 ms. Of today's 359 MiB store: 467,101,398 bytes in 6.1 s, 19,916 blobs, no hidden hits, no blobEncryptionKey, peak RSS 2.6 GB (that figure includes the test parsing the JSON back). That measurement is why the 256 MiB cap is in this ticket: the 744 MiB and 2.5 GiB stores would be several times that. With the cap, 6 of the 9 ACP sessions export now; the 359 MiB, 744 MiB and 2.5 GiB ones wait for TKT-01M44B45MT89CGR6M0HHTWG63P. Mutation checks: removing the ACP path case, the hold, the WAL mtime in the hold, the held count, the held inventory, the agent's Settle, or the cap each fails a test. The agent's call to armHold after a pass has no test of its own; armHold is tested directly.
