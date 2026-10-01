---
schema: 3
id: TKT-01M3TQNYR8TK6K67S00MZ6ZECT
title: "Release v0.5.1: transcript run folding; notes, tag, archives and image"
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
  actor: agent:claude-code/27b21f4b
  branch: release/next
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-27b21f4b
  commit: 996b66441151b331c4d6ba6f706d8353eecc61a1
  session: null
  claimed_at: 2026-10-01T03:21:13Z
  expires_at: null
archive: null
created_at: 2026-10-01T03:21:12Z
updated_at: 2026-10-01T03:21:29Z
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

Since v0.5.0 (85480f5), main changed only `internal/web`: the transcript run folding from TKT-01M3SZQ69B (Transcript page: collapse runs of unknown harness events), merged in #169. There is no diff in `internal/catalog`, `internal/normalize`, `internal/protocol`, `deploy/` or the `Dockerfile`.

- The catalog stays at schema 21, and no migration is appended.
- The normalizer, search index and protocol are unchanged, so nothing needs to be normalized or rebuilt, and agents need no upgrade.
- Rollback is swapping the binary back: v0.5.0 runs the same schema-21 catalog.

### Version

v0.5.1 is proposed, not decided. The release is a dashboard display change with no schema, protocol, config or agent change. v0.6.0 would be the choice if every feature release takes a minor, as v0.2.0 to v0.5.0 each did.

### Open before the tag

The owner runs `diagnose-v0.5.0-gH1zgIyY/diagnose.sh` (read-only) for two follow-ups from TKT-01M3SYMZCW (Deploy v0.5.0 to the internal lake and workstation agent): the `GROUP` admin mapping in role_map, and the two failed normalizations. If the failures show a normalizer bug, its fix joins this release, and the notes and the rehearsal change with it.

## Acceptance criteria

- [x] A lake at v0.5.0 (schema 21) starts on a build of the final main with nothing to migrate, passes health and fsck
- [ ] The owner confirmed the version and the cut
- [ ] The release is tagged on both forges and its archives and image name the tag
- [ ] Release notes say there is no migration, rollback is a binary swap, agents need no upgrade, and describe the run folding

## Implementation plan

1. Wait for the diagnosis. Fold in any fix it calls for, then re-run CI and the rehearsal on the final main.
2. Get the owner's confirmation of the version and the cut.
3. Tag at a commit on both mains, push to origin and github, check the archives and the image's `--version` on both forges, and prepend these notes to both release bodies (with `####` headings, as v0.4.0 and v0.5.0 did).
4. Deploy to the internal lake as a separate ticket, when the owner asks. It is a binary swap with no migration.

#### Upgrading from v0.5.0

- **Nothing to migrate.** The catalog stays at schema 21, and blobs, events files, the search index and the protocol are unchanged. To roll back, install v0.5.0 again. Agents need no upgrade: only the lake's dashboard changed.

#### New

- **The transcript page folds bookkeeping.** Three or more unknown events with no text in a row, such as Claude Code's `file-history-snapshot` and `queue-operation` records, collapse into one row that names the range and each record type, for example `4 unknown events #10–#13 file-history-snapshot ×3, queue-operation ×1`. Expanding it shows the original cards, so links, event ids and copy-out selection work as before. A link to an event inside a run opens it, and an opened run stays open across Refresh now.
- **Unknown events name their type.** Each `unknown` card shows the harness's own record type, such as `ai-title` or `system/local_command`.

## Notes

**agent:claude-code/27b21f4b** at 2026-10-01T03:21:14Z

Rehearsal, 2026-09-30: a scratch lake created by v0.4.0 and upgraded by v0.5.0 (schema 21, token-file device, viewer/operator role_map) started on a build of 996b664 (main 57f2200 plus a ticket commit). migrate --check reported 'catalog schema 21, up to date'; serve logged 'catalog schema 21, up to date' with no migration, and healthz answered. GOFLAGS=-mod=mod just ci passed on the feature branch, and main CI passed on the merged tree 57f2200. Rerun if a fix from the diagnosis lands.

**agent:claude-code/27b21f4b** at 2026-10-01T03:21:29Z

Correction to the rehearsal note: serve fsck with the 996b664 build exited 0 ('checked 0 entries, 0 bad'), but the scratch lake holds no blobs, so fsck had nothing to check. That is acceptable here only because nothing since v0.5.0 touches storage, the catalog or the normalizer.
