---
schema: 3
id: TKT-01M3558DX60C92B1YRTT4M1Y3X
title: Project linking via normalized git remote
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/catalog
  - phase/2-harness
assignees: []
milestone: phase-2
parent: TKT-01M3558DV5NHYZBFMPV1AVRNYQ
origin: null
dependencies:
  - TKT-01M3558DJXS57VSP73457Z1DGD
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:12Z
updated_at: 2026-09-23T13:24:36Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/a708
  name: Cursor cloud agent
extensions: {}
---

## Description

Layer C project_id from git_remote_normalized (+ root commit); stop relying on path CWDHash across machines.

## Acceptance criteria

- [x] project_id comes from git_remote_normalized and the root commit
- [x] The same repository on two machines links without using path CWDHash

## Implementation plan

Record Layer C on the session, derived only from the normalized origin URL and the repository root commit.

### Identity
protocol.ProjectLinkID normalizes the origin URL the same way the allowlist already does, then joins that key with the lowercase root commit. An empty remote, a non-hex root, or a missing root yields an empty id. The cwd and cwd_hash are not inputs. The lake recomputes the id on ingest and ignores a client-supplied project_id, so a path hash cannot be stored as the link.

### Root commit
The root is the first-parent commit with no parents, read from loose objects when that chain is present. Otherwise it is git rev-list --max-parents=0 --first-parent HEAD. A shallow repository does not get an id. git_commit stays HEAD. cwd_hash stays the local path bucket for the allowlist.

### Where it lands
Adapters fill git_root and project_id on the manifest. The catalog stores project_id on the session and can list the sessions that share one. The terva projection copies that id onto the event. Async normalize and the hook are untouched.

## Summary

project_id is protocol.ProjectLinkID: the origin URL folded the same way as an allow rule, then @, then the lowercase root commit. Adapters record git_root from the first parentless commit on HEAD's first-parent chain (loose objects, or git rev-list when that chain is packed). The catalog stores the id and SessionsByProject lists the sessions that share it. Ingest recomputes the id, so a client-supplied cwd hash is not kept. The terva projection copies the id onto each event. cwd_hash remains the local allowlist bucket.

Tests cover two absolute paths, ssh and https spellings, different HEADs, one shared root, and a project_id that is not either cwd hash. A second root, a second remote, a shallow clone, and a checkout with no git stay unlinked. go test ./... passed, including the MVP acceptance gate.

Async normalizer workers, parquet partitions, and the terva hook polish are still open. When this ticket landed, the synchronous projector implemented terva only. Workers now project terva, claude, codex, and opencode. A shallow clone has an empty project_id because the root commit is not in the object store.
