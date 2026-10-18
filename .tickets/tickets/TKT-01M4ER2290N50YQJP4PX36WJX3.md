---
schema: 3
id: TKT-01M4ER2290N50YQJP4PX36WJX3
title: "Release v0.9.0: dashboard navigation and lake maintenance"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/ci
  - area/ops
  - area/docs
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:codex/memory-e2fc465b
  branch: t3code/explore-lampi-memory-footprint
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-e2fc465b
  commit: e2825e9b7aee6806c92e66e663587b01bfbcd284
  session: null
  claimed_at: 2026-10-18T04:49:27Z
  expires_at: null
archive: null
created_at: 2026-10-08T21:52:38Z
updated_at: 2026-10-18T04:49:43Z
created_by:
  id: agent:codex/t3code-8afe4a1e
  name: ""
updated_by:
  id: agent:codex/memory-e2fc465b
  name: ""
extensions: {}
---

## Description

### Candidate and scope

Candidate v0.9.0, pending the owner deciding to publish. A minor version matches this repository's feature-release practice; v0.8.1 would understate the new dashboard and Operations functionality. v0.8.0 was published on 2026-10-04; before this work, v0.8.0..main contains only ticket bookkeeping, so MCP recall and Cursor CLI improvements are already released.

The release would carry TKT-01M4EPDBG32RKNM9D5MPZJQ25F — Dashboard: full pagination and Operations maintenance controls, through https://git.local.sothr.com/terva-sh/lampi/pulls/204.

### Draft release notes

Dashboard lists now have matching pagination controls above and below each list, with first/previous/next/last links, nearby page numbers, and page totals. Sessions, search, transcripts, conflicts, artifact history, and provenance preserve filters, page sizes, scopes, and transcript generation pins.

Operations administrators can request background search-index compaction, old-upload cleanup, and storage measurements. Jobs require a recent sign-in, are audited, run one at a time, and report their state and result. Full stored-blob compaction still requires the offline CLI and an exclusive lake lock; the online search action compacts the derived search index.

Lampi gains a pond icon for browser tabs and saved shortcuts, including SVG, ICO, and Apple touch formats with public discovery routes (PR #206).

Search-index maintenance recovers indexes that exhausted FTS5's 2,000 segment IDs. Repeated forced merge starts previously accumulated unfinished outputs and caused a misleading SQLite disk-full error despite available disk space. Forced merges now continue across uploads and restarts; explicit compaction transactionally rebuilds saturated full-text tables from their existing indexed rows and retries sessions whose indexing failed (PR #207). The catalog, transcripts, and index metadata remain intact.

Search remains readable when the connection pool opens readers during index maintenance or indexing writes: persistent database settings are configured once at startup rather than taking a write lock on every connection (PR #210, merged and verified on dogfooding).

Upgrading from v0.8.0: no catalog migration (schema remains 21), no normalizer changes or re-normalization, and no ingest/read protocol changes. git diff v0.8.0 -- internal/catalog/catalog.go internal/normalize is empty. Rollback to v0.8.0 is a binary/image swap without restoring a migration backup. Old-upload cleanup removes only expired partial uploads through the existing sweeper; completed maintenance does not undo itself on rollback. Upgrade the lake for the new dashboard; agents need no new configuration.

### Preparation

The full Go suite and affected-package race checks passed locally. just release-check passed. Feature PRs #204, #206, #207 and #210 passed Forgejo CI and terva review, merged, and synced to both main remotes. The current dogfooding build is fe787a4; its installed version/hash, protected checkpoint report, authentication, health and capture were verified, and the operator confirms Search works. The report records the public icon checks passing. The operator confirmed successful compaction from 3252.5 MiB to 3138.2 MiB. Search also encountered SQLITE_BUSY while opening pooled connections during maintenance; PR #210 merged and synced to both forges to address this; its rollout completed at 2026-10-09T02:06:57Z and live Search is operator-confirmed working. Both search incident tickets are done; PR #211 records the verified deployment. Publishing a tag or changing the live installation is not part of the current release assessment request.

Before publishing, rehearse the v0.8.0 lake upgrade on isolated synthetic data, run the release gates and archive builds, then tag the identical main SHA on both forges. Verify both release workflows, checksums, binary versions, and multi-platform image versions, and place the upgrade notes in both release bodies.

## Acceptance criteria

- [x] The owner selects the release version and authorizes publication.
- [ ] A scratch v0.8.0 upgrade rehearsal and release gates pass on the synchronized main commit.
- [ ] The tag is published on both forges and archives, checksums, and images verify the selected tag.
- [ ] Both release bodies describe features, unchanged schema/normalizers, and rollback.

## Implementation plan

The owner's 2026-10-17 request authorizes completing release publication, including review, merge, main synchronization, and tagging. Select v0.9.0, the existing unused feature-release candidate, now also including TKT-01M56GHAXEJJXEE45GJ9C37NQG — Bound memory and repeated work for growing transcripts. Prepare update scripts after publication; do not execute live updates under this request.

1. Finalize combined dashboard/search/memory notes, correcting the earlier draft's no-normalizer-code-change statement: implementations change, but event semantics/schema remain compatible and no full requeue or catalog migration is introduced.
2. Rehearse v0.8.0 synthetic lake upgrade. Existing full race/vet/build, five-target archives, checksums, and both image-build rehearsals passed at source-equivalent 45fea68. Open the Forgejo PR, require current CI and terva-review/code, and resolve all findings before merge.
3. Merge, synchronize both mains, annotate v0.9.0 on the identical main commit, and push the tag to both forges. Verify both publication workflows, all archives/checksums, tagged binary identity, image platforms/provenance, and release bodies.
4. Record release evidence. Prepare separately tracked, version-pinned local and fleet update scripts with backup, verification and rollback based on documented deployment configuration; no live installation or restart is implied.

## Notes

**agent:codex/t3code-8afe4a1e** at 2026-10-08T21:59:31Z

Release preparation: just release-check passed, and a GoReleaser snapshot on 397ca0d built all five archives (Linux and Darwin amd64/arm64; Windows amd64), README/LICENSE, and checksums without publication. The follow-up continuation fix is cleanly reviewed at f832e413f7cda286138eb9524197ce4b545912d6; complete web/recall race tests pass. This remains a draft release candidate for the owner's decision, with upgrade notes in the description. No tag, release, image, or live deployment was published.

**agent:codex/memory-e2fc465b** at 2026-10-18T04:49:43Z

Release scope now includes the completed memory epic as well as dashboard/search maintenance. v0.9.0 remains unused on both forges; both main branches are synchronized and are ancestors of this branch. The owner explicitly requested publication, which promotes this ticket and authorizes the merge/tag/release steps. docs/release-notes-memory.md is the combined v0.9.0 release body. This supersedes the draft description's older assertion that internal/normalize has no code changes: streaming implementations change, but schema 21, event schema 1 and normalizer semantics remain compatible, and no full requeue is added.
