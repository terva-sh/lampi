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
updated_at: 2026-10-08T21:59:31Z
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

**agent:codex/t3code-8afe4a1e** at 2026-10-08T21:54:01Z

PR #204: https://git.local.sothr.com/terva-sh/lampi/pulls/204. Initial review 2329 reviewed 6f20fd0f47b87b149d33c0e5854c491a49edc474 at https://git.local.sothr.com/terva-sh/lampi/pulls/204#issuecomment-28207 (Actions 1927).

Finding-1 claimed search uses from as its date bound. This is rejected: parseSearch only accepts since/until and rejects from, so deleting from cannot discard a supported search filter. Added end-to-end dated search first/last/previous navigation checks and explicit bound-preservation assertions. Transcript-only deletion of from/at is now scoped to generation-pinned transcript links to make the helper's intent explicit. Request a fresh review after this small code change.

Release assessment: v0.8.0 was published on 2026-10-04 and the pre-feature delta contains only ticket bookkeeping. Candidate v0.9.0 is tracked in draft TKT-01M4ER2290N50YQJP4PX36WJX3 — Release v0.9.0: dashboard navigation and lake maintenance. No schema or normalizer change since v0.8.0; release configuration check passed. No release tag publication was requested yet.

**agent:codex/t3code-8afe4a1e** at 2026-10-08T21:56:43Z

Review 2330 reviewed 397ca0d3a5dfe96087f2ccc36c3fd02aa2991131 at https://git.local.sothr.com/terva-sh/lampi/pulls/204#issuecomment-28210. It acknowledged the earlier search-date premise was incorrect and reported a real medium issue: numbered boundaries recalculated after the displayed session read can repeat or skip a record during ordering changes.

Accepted finding-1. Sequential Next now uses the displayed page's actual continuation cursor for every list, preserving keyset continuation during live ordering changes and transcript deep links between numbered boundaries. SearchNumbered retains one extra lightweight matching key so Search returns its normal actual continuation. Added a regression that inserts a leading session between the display read and navigation read and verifies the next page has the remaining two undisplayed sessions. Numbered jumps/counts continue to represent the latest navigation scan; building a database snapshot across display and navigation was rejected as unnecessary for preserving existing live keyset semantics.

The dated-search regression passed against the original reviewed pager as well as the clarified helper; disposition 28208 rejected review 2329 finding-1 with parser evidence. The complete web race suite passed. A GoReleaser snapshot on 397ca0d built and archived all five Linux/Darwin amd64/arm64 and Windows amd64 targets, with checksums, in task scratch; nothing was published.

**agent:codex/t3code-8afe4a1e** at 2026-10-08T21:59:31Z

Final full code review is clean at f832e413f7cda286138eb9524197ce4b545912d6, base cc08fc90446034dbc3680c2eea383294d925315c: https://git.local.sothr.com/terva-sh/lampi/pulls/204#issuecomment-28220 (Actions 1933, run b14f5f16-b13e-4413-bd3b-420a5e41a7b2). The reviewer confirms the continuation issue resolved and reports no new findings. Both findings have recorded dispositions at https://git.local.sothr.com/terva-sh/lampi/pulls/204#issuecomment-28219. The initial rejected keyword in comment 28208 was unsupported; valid declined command 28216 supersedes it, and accepted command 28217 records the continuation fix.

All web and recall tests pass under the race detector after the continuation fix. The concurrent-order regression fails when Next is reverted to the scanned numbered boundary and passes with the displayed continuation. Remaining changes are ticket records only, so carry the clean review to this head without another model run. The user explicitly requested merge and push; merge PR #204 after both CI jobs pass and synchronize main to GitHub with just sync-github --yes. No release tag or live rollout is authorized by the request to consider a release.

## Summary

Delivered matching top/bottom full pagination across all existing paginated dashboard lists, preserving filters, limits, scope and transcript pins. Added audited admin-only background search-index compaction, stale-upload cleanup and storage refresh, with concurrency control, status/results and shutdown coordination. Full stored-blob compaction remains offline because it requires exclusive ingest access. Documentation and synthetic browser smoke locators were updated. The full suite passes with go test -trimpath=false -p 1 ./...; affected-package race tests, vet, formatting, diff checks and production build pass. Native browser layout checks confirm the added controls fit desktop and 390px phone layouts. No live lake or agent configuration was changed.
