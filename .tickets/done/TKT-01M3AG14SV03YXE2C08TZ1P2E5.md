---
schema: 3
id: TKT-01M3AG14SV03YXE2C08TZ1P2E5
title: Cursor 1b workspace export enrichment
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/adapter
  - phase/4-cursor
assignees: []
milestone: phase-4
parent: TKT-01M3558E2PNBKNHFY6J7A37GJ4
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-24T19:59:39Z
updated_at: 2026-09-24T20:06:44Z
created_by:
  id: agent:cursor/0888
  name: Cursor cloud agent
updated_by:
  id: agent:cursor/0888
  name: Cursor cloud agent
extensions: {}
---

## Description

Enrich harness `cursor` workspace exports with the composers that belong to that workspace, taken from the global `state.vscdb`.

The adapter already builds `cursor_state_json` by copying a workspace `state.vscdb` (and its WAL sidecars) and reading it read-only. Chat bodies for current Cursor builds live in the global database's `cursorDiskKV`, not in the workspace file. A workspace export that only reads the workspace file therefore has no type 1 or type 2 bubbles.

When building a workspace document, snapshot the sibling global `state.vscdb` the same way (`copyTrio`, then a read-only open of the copy). Filter that global `cursorDiskKV` to composers named by this workspace's ItemTable key `composer.composerHeaders` (`allComposers[].composerId`). Merge those rows into the workspace document's `cursor_disk_kv`. `composer.composerData` is the older workspace list and is not the registry.

The session id stays `workspace/<id>`. The global database still has an empty cwd, so an export of the global file alone is still refused and is not uploaded. Confidence stays low. Keys under `cursorAuth/` stay stripped. Bump the adapter `Version`.

This is adapter enrichment only. It does not wire soft-link, deploy, or AgentsView. It does not promote `toolFormerData`, `toolResults`, `usageData`, or `latestConversationSummary`. It does not read the Cursor CLI `store.db`, open a live Cursor install, or add a PlantCursor synthetic gate.

## Acceptance criteria

- [x] A workspace cursor_state_json snapshots global state.vscdb read-only and merges cursorDiskKV rows for composers named by composer.composerHeaders
- [x] Session id stays workspace/<id>, and a global database alone is still not uploaded
- [x] Composers from other workspaces, and composers listed only on composer.composerData, are absent from the allowlisted export
- [x] Adapter Version is bumped, confidence stays low, and cursorAuth/* stays stripped
- [x] export --format events on an allowlisted workspace whose bubbles live only on the global database yields type 1 and type 2 content_text, and session_id is cursor:workspace/<id>
- [x] A wrong artifact kind records normalize_error, and encrypted, cipher, and sealed values stay out of content_text
- [x] go test ./... is green with a dual-DB fixture and does not open a live Cursor install

## Definition of done

- [x] Enrichment lives in the existing Cursor harness adapter
- [x] The dual-DB fixture locks composer.composerHeaders and does not treat composer.composerData as the registry
- [x] Soft-link, deploy, AgentsView, toolFormerData promotion, cursor-cli store.db, live DB opens, and PlantCursor are unchanged

## Implementation plan

Extend the existing Cursor IDE adapter in `internal/adapter/cursor`, on the path that already builds `cursor_state_json`.

### Registry
The normalize fixtures and the worker's cursor document already use ItemTable key `composer.composerHeaders` with `allComposers[].composerId` as the composer index. `composer.composerData` is the older workspace list (`TestCursorItemTableOnlyWorkspaceIndex`). Membership is the headers key only. A composer listed only on `composer.composerData` is not merged.

### Snapshot
`Manifests` and `ReadSlice` already call `exportDatabase`. When the source path is `User/workspaceStorage/<id>/state.vscdb` and that workspace's headers name at least one composer, `copyTrio` the sibling `User/globalStorage/state.vscdb` and open the copy read-only. A missing global file adds nothing. A copy or open failure fails the workspace export. The live files are not opened.

### Merge
Keep the workspace document's own `cursor_disk_kv` rows. Append global rows whose composer id, taken from `bubbleId`, `composerData`, `checkpointId`, `messageRequestContext`, or `codeBlockDiff`, is in the headers set. Drop `cursorAuth/*` with the existing key filter. Sort by key. Do not copy the rest of the global table.

### Unchanged
Session id stays `workspace/<id>`. The global manifest still has an empty cwd and the allowlist still refuses it. Confidence stays `low`. `Version` becomes `2`. The normalizer is not asked to promote tool, usage, or summary fields.

## Summary

Workspace `cursor_state_json` snapshots the global `state.vscdb` read-only and merges `cursorDiskKV` rows for composers named by the workspace ItemTable key `composer.composerHeaders` (`allComposers[].composerId`). `composer.composerData` is not the registry. Adapter Version is 2. Confidence stays low. `cursorAuth/*` stays stripped. The session id stays `workspace/<id>`. A global database alone is still refused and is not uploaded.

`gofmt -l` was clean, `go vet ./...` passed, and `go test ./...` passed. The dual-DB fixture is `TestCursorWorkspaceEnrichmentPass` and `TestWorkspaceMergesComposerHeaders`. Both use synthetic sqlite files under a temp directory. They do not open a live Cursor install.

Landed on cursor/cursor-1b-workspace-export-0888 as https://github.com/terva-sh/lampi/pull/53.
