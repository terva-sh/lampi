---
schema: 3
id: TKT-01M3TQNYR8TK6K67S00MZ6ZECT
title: "Release v0.5.1: transcript run folding, torn-line fix; tag and notes"
type: task
status: done
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
created_at: 2026-10-01T03:21:12Z
updated_at: 2026-10-01T05:24:44Z
created_by:
  id: agent:claude-code/27b21f4b
  name: ""
updated_by:
  id: agent:claude-code/27b21f4b
  name: ""
extensions: {}
---

## Description

The owner asked on 2026-09-30 to prepare the next release after v0.5.0. That request is this ticket's promotion. The tag waits for the owner to confirm the cut and the version.

### What it carries

Since v0.5.0 (85480f5), main gained two changes:

- **#169, TKT-01M3SZQ69B** (Transcript page: collapse runs of unknown harness events). Dashboard only, in `internal/web`.
- **#170, TKT-01M3NQ2R** (Normalize: a torn line mid-file fails the whole session). This changes `internal/normalize` for Claude, Codex and terva JSONL: a line that is not a JSON object becomes an error event instead of failing the session, and more than 64 such lines still fail the file.

There is no diff in `internal/catalog`, `internal/protocol`, `deploy/` or the `Dockerfile`.

- The catalog stays at schema 21, and no migration is appended.
- **Normalizer output** changes only for sessions that previously failed. A session with no bad line projects exactly as before, so ready sessions need no re-projection. Sessions that failed on a torn line recover with `serve normalize --failed` and a SIGHUP.
- The search index format and the protocol are unchanged, and agents need no upgrade.
- **Rollback** is swapping the binary back, because v0.5.0 runs the same schema-21 catalog. A session recovered by the retry keeps its derived files under v0.5.0, but v0.5.0 would fail it again if it were re-projected.

### Version

v0.5.1 is proposed, not decided. The release is a bug fix plus a dashboard display change, with no schema, protocol, config or agent change. v0.6.0 would be the choice if every feature release takes a minor, as v0.2.0 to v0.5.0 each did.

### Diagnosis it came from

The owner ran `diagnose-v0.5.0-gH1zgIyY/diagnose.sh` (read-only) for two follow-ups from TKT-01M3SYMZCW (Deploy v0.5.0 to the internal lake and workstation agent):
- **role_map** mapped a group literally named `GROUP` to admin. The owner chose to make `Brokkr Lampi Admin` admin instead, and the deploy applies it.
- **The two failed normalizations** are the torn lines of TKT-01M3NQ2R, which #170 fixes.

## Acceptance criteria

- [x] A lake at v0.5.0 (schema 21) starts on a build of the final main with nothing to migrate, passes health and fsck
- [x] The owner confirmed the version and the cut
- [x] The release is tagged on both forges and its archives and image name the tag
- [x] Release notes say there is no migration, rollback is a binary swap, agents need no upgrade, and describe the run folding

## Implementation plan

1. Merge #170 (TKT-01M3NQ2R, Normalize: a torn line mid-file fails the whole session). Then re-run CI and the rehearsal on the final main.
2. Get the owner's confirmation of the version and the cut.
3. Tag at a commit on both mains, push to origin and github, check the archives and the image's `--version` on both forges, and prepend these notes to both release bodies (with `####` headings, as v0.4.0 and v0.5.0 did).
4. Deploy to the internal lake as a separate ticket, when the owner asks. It is a binary swap with no migration, plus three steps:
   - **role_map:** `Brokkr Lampi Admin` goes from operator to admin and `GROUP` is removed. The owner chose this on 2026-10-01.
   - **Retry:** `serve normalize --failed` with a SIGHUP, to re-project the two sessions TKT-01M3NQ2R left failed.
   - **Restart:** the restart picks up the role_map change.

#### Upgrading from v0.5.0

- **Nothing to migrate.** The catalog stays at schema 21, and blobs, the search index and the protocol are unchanged. To roll back, install v0.5.0 again. Agents need no upgrade: only the lake changed.
- **Sessions that failed on a torn line can be recovered.** After upgrading, run `terva-lampi serve normalize --failed` and send serve a SIGHUP (or restart it). Sessions that failed with `line N is not a JSON object` because of one or a few bad lines then normalize.

#### New

- **The transcript page folds bookkeeping.** Three or more unknown events with no text in a row, such as Claude Code's `file-history-snapshot` and `queue-operation` records, collapse into one row that names the range and each record type, for example `4 unknown events #10–#13 file-history-snapshot ×3, queue-operation ×1`. Expanding it shows the original cards, so links, event ids and copy-out selection work as before. A link to an event inside a run opens it, and an opened run stays open across Refresh now.
- **Unknown events name their type.** Each `unknown` card shows the harness's own record type, such as `ai-title` or `system/local_command`.

#### Fixed

- **One torn line no longer fails a whole session.** A line in a Claude Code, Codex or terva transcript that is not a JSON object becomes an error event in its place. The event names the line number and its length, never its bytes. A harness can write such a line itself: Claude Code once cut a record off mid-string and wrote the next record on the same line. The record joined to a torn line is skipped with it. A file with no JSON object line, or with more than 64 bad ones, still fails as not JSONL.

## Notes

**agent:claude-code/27b21f4b** at 2026-10-01T03:21:14Z

Rehearsal, 2026-09-30: a scratch lake created by v0.4.0 and upgraded by v0.5.0 (schema 21, token-file device, viewer/operator role_map) started on a build of 996b664 (main 57f2200 plus a ticket commit). migrate --check reported 'catalog schema 21, up to date'; serve logged 'catalog schema 21, up to date' with no migration, and healthz answered. GOFLAGS=-mod=mod just ci passed on the feature branch, and main CI passed on the merged tree 57f2200. Rerun if a fix from the diagnosis lands.

**agent:claude-code/27b21f4b** at 2026-10-01T03:21:29Z

Correction to the rehearsal note: serve fsck with the 996b664 build exited 0 ('checked 0 entries, 0 bad'), but the scratch lake holds no blobs, so fsck had nothing to check. That is acceptable here only because nothing since v0.5.0 touches storage, the catalog or the normalizer.

**agent:claude-code/27b21f4b** at 2026-10-01T04:59:05Z

Scope grew on 2026-10-01: the release now carries TKT-01M3NQ2R (torn line becomes a marker, capped at 64) from #170, which changes internal/normalize. Criterion 1 is unticked until the rehearsal is re-run on the final main. Normalize output changes only for sessions that previously failed, so ready sessions need no re-projection.

**agent:claude-code/27b21f4b** at 2026-10-01T05:03:08Z

### Rehearsal on the final main, 2026-10-01

This supersedes the 2026-09-30 rehearsal and its fsck caveat. The build was 74ebae3: main 759a12d with #170, plus this branch's ticket commits.

- **Seeded with v0.5.0.** The v0.5.0 release binary served a scratch lake, and a scratch agent uploaded a Claude transcript whose line 3 is a cut-off record with the next one joined on, the shape of the internal lake's line 691. It failed with `normalize: line 3 is not a JSON object`. Counts: 1 session, 1 artifact, 1 provenance row.
- **Upgraded.** `migrate --check` with the new build reported `catalog schema 21, up to date`, and serve logged the same at start, with no migration.
- **Recovered live.** `serve normalize --failed` queued the session, and a SIGHUP to the running serve projected it: ready=1, failed=0. The recovery works without a restart.
- **Agent.** The next sync uploaded nothing (unchanged 1).
- **Checks.** Counts are unchanged (1, 1, 1), integrity is ok, and `serve fsck` reports 1 entry, 0 bad.

`GOFLAGS=-mod=mod just ci` passed on #170's head, and Forgejo CI and terva-review passed on 3c40344.

**agent:claude-code/27b21f4b** at 2026-10-01T05:17:46Z

Owner confirmed the cut as v0.5.1 and asked for a deploy afterwards (2026-10-01). Tagged 4ef0c50 (annotated), main on both forges; main CI passed on 4ef0c50, and GitHub main was fast-forwarded to it. Pushed to origin and github. Review record for #171: review 1638 on c8d1001 found the stale scope description (medium). It was fixed in 3bdcd7e with disposition finding-1 accepted, and the scope-fix review of 3bdcd7e found nothing. #170 had clean reviews of 5cf2215 and 3c40344.

**agent:claude-code/27b21f4b** at 2026-10-01T05:24:44Z

Published 2026-10-01. GitHub release run 36819064159 passed: 6 assets, not a prerelease, and the linux_amd64 archive matches checksums.txt and reports 'terva-lampi v0.5.1 (4ef0c505de03)'. ghcr.io/terva-sh/lampi:0.5.1 reports 'terva-lampi v0.5.1 (4ef0c50)'. The Forgejo release (id 17618) passed both jobs, has 6 assets, and its linux_amd64 binary reports the tag. Notes prepended to both bodies (#### Upgrading from v0.5.0 / New / Fixed).

## Summary

v0.5.1 is tagged at 4ef0c50 and published on GitHub (archives, plus ghcr.io/terva-sh/lampi:0.5.1) and on Forgejo (archives). Each archive and the image report v0.5.1. It carries the transcript run folding (#169) and the torn-line marker capped at 64 (#170), with no migration. The notes cover the binary-swap rollback and recovering failed sessions with normalize --failed and a SIGHUP. Deployed in TKT-01M3TYFVD9.
