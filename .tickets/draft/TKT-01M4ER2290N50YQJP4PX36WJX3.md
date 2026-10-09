---
schema: 3
id: TKT-01M4ER2290N50YQJP4PX36WJX3
title: "Release v0.9.0: dashboard navigation and lake maintenance"
type: task
status: draft
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
claim: null
archive: null
created_at: 2026-10-08T21:52:38Z
updated_at: 2026-10-09T01:40:09Z
created_by:
  id: agent:codex/t3code-8afe4a1e
  name: ""
updated_by:
  id: agent:codex/t3code-8afe4a1e
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

Search remains readable when the connection pool opens readers during index maintenance or indexing writes: persistent database settings are configured once at startup rather than taking a write lock on every connection (PR #210; merge and dogfooding rollout pending).

Upgrading from v0.8.0: no catalog migration (schema remains 21), no normalizer changes or re-normalization, and no ingest/read protocol changes. git diff v0.8.0 -- internal/catalog/catalog.go internal/normalize is empty. Rollback to v0.8.0 is a binary/image swap without restoring a migration backup. Old-upload cleanup removes only expired partial uploads through the existing sweeper; completed maintenance does not undo itself on rollback. Upgrade the lake for the new dashboard; agents need no new configuration.

### Preparation

The full Go suite and affected-package race checks passed locally. just release-check passed. Feature PRs #204, #206 and #207 passed Forgejo CI and terva review, merged, and synced to both main remotes. The current dogfooding build is d7dcde1; its installation, authentication, health, public icons and capture were verified. The operator confirmed successful compaction from 3252.5 MiB to 3138.2 MiB. Search also encountered SQLITE_BUSY while opening pooled connections during maintenance; PR #210 addresses this, and live healthy-search verification remains pending. Publishing a tag or changing the live installation is not part of the current release assessment request.

Before publishing, rehearse the v0.8.0 lake upgrade on isolated synthetic data, run the release gates and archive builds, then tag the identical main SHA on both forges. Verify both release workflows, checksums, binary versions, and multi-platform image versions, and place the upgrade notes in both release bodies.

## Acceptance criteria

- [ ] The owner selects the release version and authorizes publication.
- [ ] A scratch v0.8.0 upgrade rehearsal and release gates pass on the synchronized main commit.
- [ ] The tag is published on both forges and archives, checksums, and images verify the selected tag.
- [ ] Both release bodies describe features, unchanged schema/normalizers, and rollback.

## Notes

**agent:codex/t3code-8afe4a1e** at 2026-10-08T21:59:31Z

Release preparation: just release-check passed, and a GoReleaser snapshot on 397ca0d built all five archives (Linux and Darwin amd64/arm64; Windows amd64), README/LICENSE, and checksums without publication. The follow-up continuation fix is cleanly reviewed at f832e413f7cda286138eb9524197ce4b545912d6; complete web/recall race tests pass. This remains a draft release candidate for the owner's decision, with upgrade notes in the description. No tag, release, image, or live deployment was published.
