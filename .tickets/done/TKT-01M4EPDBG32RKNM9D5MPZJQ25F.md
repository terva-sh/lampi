---
schema: 3
id: TKT-01M4EPDBG32RKNM9D5MPZJQ25F
title: "Dashboard: full pagination and Operations maintenance controls"
type: task
status: done
status_reason: Completed the user-requested dashboard changes on t3code/pagination-page-controls in commit 617064a; full tests, race checks, vet and build passed.
priority: normal
due_on: null
labels:
  - area/server
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-08T21:23:50Z
updated_at: 2026-10-08T21:40:24Z
created_by:
  id: agent:codex/t3code-8afe4a1e
  name: ""
updated_by:
  id: agent:codex/t3code-8afe4a1e
  name: ""
extensions: {}
---

## Description

User requested first, previous, nearby page numbers, next and last controls above and below every paginated list, plus Operations controls for compaction and cleanup.

## Acceptance criteria

- [x] All existing paginated dashboard lists have matching top and bottom controls with nearby page numbers and first/last navigation.
- [x] Pagination preserves filters, limits, scope and transcript generation checks.
- [x] Operations offers guarded maintenance requests with progress and results using existing lake routines.

## Implementation plan

Add a shared accessible pager backed by lightweight scoped cursor-boundary queries, caching transcript boundaries by snapshot and page size. Preserve API cursors and URL filters. Add admin-only asynchronous search-index compaction, stale-upload cleanup and storage sampling through existing routines; keep CAS version folding offline because Compact requires the exclusive lake lock and no concurrent ingest. Verify page boundaries, scope, generation changes, maintenance authorization, CSRF, concurrency and result reporting.

## Notes

**agent:codex/t3code-8afe4a1e** at 2026-10-08T21:38:34Z

Implemented shared top/bottom numbered controls for sessions, search hits, transcripts, conflicts, artifact history and provenance. Links retain filters and page size; transcript links pin generation and use actual byte-bounded page starts. Lightweight ordering-key reads provide counts without collecting list content. Transcript boundaries are cached by snapshot and page size. Keeping API keyset cursors avoids an API migration and preserves existing deep links; client-side page history was rejected because it cannot provide a dependable last-page link.

Operations now has admin-only asynchronous search-index compaction, upload-leftover cleanup and storage sampling. Requests require CSRF and a recent sign-in, are audited, serialize jobs, show completion/failure, and are cancelled/joined on shutdown. Search compaction shares a writer lock with indexing passes; upload sweeping shares the partial-upload store lock. Full CAS version folding stays offline: its existing exclusive-lock requirement protects manifests and ingest, so invoking it directly from a live dashboard was rejected.

New navigation, byte-limit, scope/stale-generation, maintenance authorization/admission/shutdown and online-compaction tests pass. The affected web/recall/CAS/catalog packages pass under the race detector; vet, formatting and a production build pass. Native shared-browser layout checks used rendered synthetic pages (with CSRF fields removed) because the browser cannot reach this machine's loopback fixture. Pager and maintenance containers fit a 390px phone viewport. Existing unrelated header/document overflow remains outside this change. The full suite is being rerun with an explicit -trimpath=false because local GOFLAGS=-trimpath breaks existing runtime.Caller-based repository-file tests; clearing GOFLAGS alone did not override the local setting.

**agent:codex/t3code-8afe4a1e** at 2026-10-08T21:40:24Z

draft to done: Completed the user-requested dashboard changes on t3code/pagination-page-controls in commit 617064a; full tests, race checks, vet and build passed.

## Summary

Delivered matching top/bottom full pagination across all existing paginated dashboard lists, preserving filters, limits, scope and transcript pins. Added audited admin-only background search-index compaction, stale-upload cleanup and storage refresh, with concurrency control, status/results and shutdown coordination. Full stored-blob compaction remains offline because it requires exclusive ingest access. Documentation and synthetic browser smoke locators were updated. The full suite passes with go test -trimpath=false -p 1 ./...; affected-package race tests, vet, formatting, diff checks and production build pass. Native browser layout checks confirm the added controls fit desktop and 390px phone layouts. No live lake or agent configuration was changed.
