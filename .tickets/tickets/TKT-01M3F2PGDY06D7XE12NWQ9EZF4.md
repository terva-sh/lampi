---
schema: 3
id: TKT-01M3F2PGDY06D7XE12NWQ9EZF4
title: "Web: browse normalized transcripts with generation-safe paging"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/normalize
assignees: []
milestone: null
parent: TKT-01M3F2PGA1EFCEPEBPT1JFR3JJ
origin: null
dependencies:
  - TKT-01M3F2FSTF28GNEDGQ0XBSZ44W
blocks_on: none
references:
  - ref: plan:web-ui
    path: docs/web-ui-plan.md
claim:
  actor: agent:claude-code/cd41c9ac
  branch: t3code/explore-store-ui-search
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-946c2db7
  commit: 1c74bfb433e0975d08edd7d26848d57960e28cb5
  session: null
  claimed_at: 2026-09-26T20:36:18Z
  expires_at: null
archive: null
created_at: 2026-09-26T14:42:51Z
updated_at: 2026-09-26T20:46:19Z
created_by:
  id: agent:codex/web-ui-planning
  name: ""
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

### Scope and rationale

Provide an authenticated viewer page and bounded event API for one session. Use published normalized JSONL, not raw CAS, and never launch normalization from a read. Follow the release B generation/position cursor and unavailable-state contract. Messages, tools, compactions, errors and usage keep their source order and nullable fields. Encrypted content is opaque and never decrypted.

### Contract

Follow docs/web-ui-plan.md release B and its pinned sibling references. Use only isolated synthetic data. New work remains draft pending promotion.

## Acceptance criteria

- [x] Current-generation pages are bounded by count and bytes and carry cursors that cannot mix generations; oversized text is explicitly marked as a preview.
- [x] Pending/failed/unknown/missing output and concurrent generation changes return explicit unavailable/reload results without triggering workers.
- [x] Viewer displays messages/tools/compactions/null usage safely and preserves order, provenance and opaque encrypted-content semantics.
- [x] Tests cover concurrent publication/purge, malicious and large content, unauthorized access and no raw-path/CAS endpoint.

## Definition of done

- [ ] Focused tests and relevant API/operator docs are complete; record evidence and decisions in the ticket.

## Implementation plan

New package internal/recall holds the query layer the web API and the planned MCP server share (recall epic TKT-01M3FPP3, decision 1). Its Reader serves pages of the published normalized JSONL by event position.

### Generation pinning
The reader reads the publication state (new catalog.Publication), opens normalized/<uid>.jsonl, then reads the state again. A worker bumps normalize_gen at enqueue, before it replaces the file, and removes the file before it records a failure. So a ready state that is unchanged on both sides of the open proves which generation the descriptor holds. A rename after the open does not change what the descriptor reads. A change triggers one retry, then unavailable or generation_changed.

### Cursors
Cursors are HMAC-signed with a per-process key and carry the session, generation, head, file size and mtime, position and byte offset. Size and mtime catch a same-generation rewrite (StoreEvents from export). The signature stops a client from moving the offset off the line its position names. A restart invalidates cursors, as it does browser sessions.

### Bounds
Pages hold 100 events by default and 200 at most, with a 1 MiB item budget. content_text is cut at 32 KiB on a rune boundary, extra objects over 16 KiB are dropped, and lines over 16 MiB become placeholders. Every reduction is flagged. Encrypted, cipher and sealed extra keys are stripped and flagged, never decrypted.

### Surfaces
- GET /api/web/v1/sessions/{uid}/events?from&limit&cursor&gen. gen pins the generation; 0 is valid.
- GET /sessions/{uid}/transcript?from&cursor&gen&at, with at as a deep-link target.

## Notes

**agent:claude-code/cd41c9ac** at 2026-09-26T20:46:19Z

### Decisions and rejected alternatives

- **Addressing by line position with a signed byte-offset cursor, not a sidecar line index.** A position lookup without a cursor scans newlines. That is fast enough for transcripts of tens of MB and needs no new derived file to keep in step with publication. Revisit if deep links into very large sessions get slow. The FTS index (TKT-01M3F2PGH) could store byte offsets per position if needed.
- **Unsigned cursors (the catalog dashboard's style) were rejected.** A tampered offset would silently mislabel positions, and deep links built from them would be wrong.
- **No new catalog migration.** The onboarding branch adds migrateLakeMeta as migration 4. Avoiding a migration here keeps this work mergeable in either order.
- **Generation 0 is real.** It is what a session published without an enqueue gets (smoke fixture, older paths). Pinning is therefore an explicit flag, not gen != 0. The browser smoke caught this.
- **The page returns HTTP 409 with HTML for unavailable and stale states.** The JSON API uses 409 transcript_unavailable with a state, and 409 generation_changed.

### Evidence
- `go test -race ./internal/recall ./internal/web ./internal/catalog` passes.
- TestPagesNeverMixGenerations republishes while reading. Each run served 50 pages across about 2,000 generation changes, and no page mixed generations.
- `just ci` passes.
- The browser smoke (e2e/web-smoke.mjs, extended) passed. It covers transcript paging, truncation and opaque hints, tool error badges, deep-link focus, 390px layout and no-JS paging.
