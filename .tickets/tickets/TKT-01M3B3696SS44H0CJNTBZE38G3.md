---
schema: 3
id: TKT-01M3B3696SS44H0CJNTBZE38G3
title: "Artifact model: OpenCode snapshot kind, one head per session"
type: bug
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area/catalog
  - area/adapter
  - area/protocol
assignees: []
milestone: null
parent: TKT-01M3B35JS8J2F83FG4J7ZC0199
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-25T01:34:31Z
updated_at: 2026-09-25T01:34:31Z
created_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
updated_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
extensions: {}
---

## Description

Two artifact-model decisions are persisted in the catalog, so they should be settled before real data lands.

### Findings

- OpenCode re-exports are always `divergent_copy`. Proven. An `opencode export` document is one JSON object, so a re-export with more messages is never a byte prefix of the previous one. `snapshotArtifact` (`internal/catalog/catalog.go:503-510`) treats the Cursor kinds as snapshots but not OpenCode's `transcript_jsonl` (`internal/adapter/opencode/opencode.go:98`). The head stays at the first export, normalize never sees later messages, and each re-export stores another full blob.
- Same session, new relpath. Proven. Relpaths embed the cwd, so the same `native_session_id` from a second machine or a moved home arrives as a new relpath. `catalog.go:444-452` looks up current per relpath, so it becomes a second current head. `Project` (`internal/api/project.go:391`) concatenates every current transcript, so export and training rows hold both copies, and `head_sha256` flips with each post.

### Approach

Give the OpenCode export a snapshot kind, head-bearing like the Cursor kinds. The kind name is a protocol value, so it goes in `docs/protocol.md`. For head-bearing kinds with a new relpath, compute the relation against the session's current head. `Project` reads the head transcript and the sidecars at the head's relpath only.

### Question

The kind name. `opencode_export_json` is the proposal.

## Acceptance criteria

- [ ] Successive OpenCode exports move the head and normalize sees the later messages
- [ ] The same session posted under a second relpath leaves one current head, and Project reads it once
- [ ] docs/protocol.md names the new kind
