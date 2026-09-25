---
schema: 3
id: TKT-01M3B3696SS44H0CJNTBZE38G3
title: "Artifact model: OpenCode snapshot kind, one head per session"
type: bug
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area/catalog
  - area/adapter
  - area/protocol
  - question
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
updated_at: 2026-09-25T02:33:36Z
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
- Same session, new relpath. Proven. Relpaths embed the cwd, so the same `native_session_id` from a second machine or a moved home arrives as a new relpath. `catalog.go:444-452` looks up current per relpath, so it becomes a second current head. `Project` (the `for _, a := range arts` loop at `internal/api/project.go:53-61`) concatenates every current transcript, so export and training rows hold both copies, and `head_sha256` flips with each post.

### Approach

Give the OpenCode export a snapshot kind, head-bearing like the Cursor kinds. The kind name is a protocol value, so it goes in `docs/protocol.md`. For head-bearing kinds with a new relpath, compute the relation against the session's current head. `Project` reads the head transcript and the sidecars at the head's relpath only.

### Question

The kind name. `opencode_export_json` is the proposal.

## Acceptance criteria

- [x] Successive OpenCode exports move the head and normalize sees the later messages
- [x] The same session posted under a second relpath leaves one current head, and Project reads it once
- [x] docs/protocol.md names the new kind

## Implementation plan

### Kind
Add `protocol.KindOpenCodeExportJSON = "opencode_export_json"`. The OpenCode adapter emits it for export JSON. The database-file fallback gets a local label, `opencode_db`, defined in the adapter: the allowlist refuses that blob, so it is never a manifest kind the lake should accept, and it is no longer mislabelled as JSONL. `snapshotArtifact` treats the export kind as head-bearing like the Cursor kinds, so a re-export replaces the head instead of becoming divergent_copy.

### One head per session
In `reviseDecisions`, when the manifest's head artifact (the index `transcriptIndex` already picks) has no current row at its own relpath, and the session head row is a head-bearing kind (transcript_jsonl, both Cursor kinds, the OpenCode export) of the same kind at another relpath, compare against that head row. unchanged and stale record nothing and return the head row's artifact id. grown_from and a snapshot rewrite store the new row as current and clear current on the old head row, so the head moves to the new relpath. A non-prefix transcript is divergent_copy and the head stays. `Decision` gains `Base`, the relpath compared against, which `applyArtifact` uses for the id lookup and for clearing current.

Only the head artifact gets this rule. Other artifacts stay keyed by relpath: Claude subagent transcripts share the session id and are legitimately separate files, terva error sidecars, raati and tasks have their own paths, and Codex history.jsonl stays out by name.

### Project
Add `Catalog.Head` (uid, head digest, current rows, read in one transaction). `Project` reads the head row, plus, for terva, Claude and Codex, the current rows at or under the head's directory (error sidecars, subagent files). A second machine's rows under another directory are not read. Cursor, Cursor CLI and OpenCode read the head row only. OpenCode accepts `opencode_export_json` and the legacy `transcript_jsonl`; a session with neither is an error, like Cursor. `export --format sharegpt` points at the head digest.

### Adapter ordering
Within one OpenCode session group, sort exports by mtime so the newest export is last and becomes the head.

### Docs and tests
docs/protocol.md names the kind and the new-relpath rule; architecture and README where they describe OpenCode kinds. Tests: catalog unit tests for the moved head, OpenCode re-export through the API (head moves, derived JSONL has the later message), two machines under different relpaths (one current head, one projection), Claude subagent still projected.

## Notes

**agent:claude-code/eh1m** at 2026-09-25T02:31:25Z

### Decisions that differ from or extend the proposal

- The kind is `protocol.KindOpenCodeExportJSON = "opencode_export_json"`, one constant, so a rename in review is a one-line change plus docs.
- The database fallback is labelled `opencode_db` (`opencode.KindDatabase`), not a protocol kind. It was `transcript_jsonl`, which misdescribed a SQLite file. The allowlist refuses it before a manifest, and once the manifest kind allowlist from the catalog-versioning work lands, the lake would refuse it too. It should not be added to that allowlist.
- The new-relpath rule applies only to the manifest's head artifact (the index `transcriptIndex` already picks). Applying it to every transcript would compare Claude subagent transcripts, which share the session id, with the main transcript and store them as divergent copies.
- `Project` reads the head plus current rows at or under the head's directory for terva, Claude and Codex (error sidecars, subagents). A second machine's sidecars and subagent copies under its own directory are not read. Cursor, Cursor CLI and OpenCode read the head only.
- The projector accepts `transcript_jsonl` for OpenCode as well, so an agent older than the kind still projects. A re-export from such an agent is still a divergent copy; the catalog stays kind-based and does not special-case the harness.
- `head_machines` on /v1/conflicts now lists machines that posted the head digest under any path, since a copy from a second machine sits under a different relpath than the head.

### Known trade-off

A transcript under a new relpath that is not a prefix either way of the head is a divergent copy even when it comes from the same machine. Two Codex rollouts that share a session id in different day directories: the one that is not the head is no longer projected. Codex resume appends to the original rollout, so this should be rare.

## Summary

The OpenCode adapter emits opencode_export_json (one constant in internal/protocol); the catalog treats it as a head-moving snapshot, so a re-export replaces the head. The database fallback is labelled opencode_db, local to the adapter. The manifest's head artifact under a new relpath is compared with the session head of the same kind: unchanged and stale name the head row, a move clears current on the old path, a non-prefix transcript is a divergent copy. Project reads the head (Catalog.Head) plus current rows under the head's directory for terva, Claude and Codex, and the head only for Cursor, Cursor CLI and OpenCode; OpenCode also projects legacy transcript_jsonl. Tests in internal/catalog/head_test.go and internal/api/head_test.go. docs/protocol.md, architecture and README name the kind.
