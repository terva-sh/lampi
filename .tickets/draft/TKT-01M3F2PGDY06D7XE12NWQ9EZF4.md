---
schema: 3
id: TKT-01M3F2PGDY06D7XE12NWQ9EZF4
title: "Web: browse normalized transcripts with generation-safe paging"
type: task
status: draft
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
claim: null
archive: null
created_at: 2026-09-26T14:42:51Z
updated_at: 2026-09-26T14:42:52Z
created_by:
  id: agent:codex/web-ui-planning
  name: ""
updated_by:
  id: agent:codex/web-ui-planning
  name: ""
extensions: {}
---

## Description

### Scope and rationale

Provide an authenticated viewer page and bounded event API for one session. Use published normalized JSONL, not raw CAS, and never launch normalization from a read. Follow the release B generation/position cursor and unavailable-state contract. Messages, tools, compactions, errors and usage keep their source order and nullable fields. Encrypted content is opaque and never decrypted.

### Contract

Follow docs/web-ui-plan.md release B and its pinned sibling references. Use only isolated synthetic data. New work remains draft pending promotion.

## Acceptance criteria

- [ ] Current-generation pages are bounded by count and bytes and carry cursors that cannot mix generations; oversized text is explicitly marked as a preview.
- [ ] Pending/failed/unknown/missing output and concurrent generation changes return explicit unavailable/reload results without triggering workers.
- [ ] Viewer displays messages/tools/compactions/null usage safely and preserves order, provenance and opaque encrypted-content semantics.
- [ ] Tests cover concurrent publication/purge, malicious and large content, unauthorized access and no raw-path/CAS endpoint.

## Definition of done

- [ ] Focused tests and relevant API/operator docs are complete; record evidence and decisions in the ticket.

## Implementation plan

Inspect publication locks and atomic rename in internal/api/worker.go and internal/normalize/export.go. Introduce a shared reader snapshot abstraction that pins generation and acquires the matching file consistently with publication; later export and indexing reuse it. Default 100 events/page, max 200, maximum 1 MiB response page; represent oversize content with an explicit truncated preview rather than loading an unbounded row into a response. A generation mismatch returns 409/reload, unavailable projection returns a documented non-success status, unknown UID is 404. Treat content as plain text, not HTML/Markdown execution. Test concurrent publish/failure/purge/restart, long rows, malicious content, stable ordering and bounded reads. Add accessible session transcript navigation and exact API documentation.
