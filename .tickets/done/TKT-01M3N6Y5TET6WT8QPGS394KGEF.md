---
schema: 3
id: TKT-01M3N6Y5TET6WT8QPGS394KGEF
title: "Dashboard: storage card compares blobs with raw transcripts"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/cas
assignees: []
milestone: null
parent: TKT-01M3K45MQBRESGG5HEZZSZ3FDQ
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T23:52:24Z
updated_at: 2026-09-29T01:39:06Z
created_by:
  id: agent:claude-code/d8436f9f
  name: ""
updated_by:
  id: agent:claude-code/d8436f9f
  name: ""
extensions: {}
---

## Description

The dashboard's Deduplication card divides the bytes every artifact row
names by the stored blobs' disk use. Every version of a growing
transcript is a row counted in full, so the number mostly counts
continuations: a lake of long sessions reads 50× or more, and none of it
is duplicate content. Byte-identical files are rare.

Replace it with what the storage epic measures itself against: the
current version of every path, which is what the machines hold raw,
against the stored blobs' disk use. Report true duplicates, current
files whose digest another current file shares, beside it.

- Sample two new measures, `artifacts.current` and
  `artifacts.current_unique`, from the `current` flag on artifact rows.
- The card shows raw bytes ÷ blob disk use, raw and on-disk sizes, and
  the duplicate count and bytes.
- Export both as Prometheus gauges.
- Keep the referenced and unique measures; the notes say what they are.

## Acceptance criteria

- [x] The card divides the current versions' bytes by the stored blobs' disk use
- [x] Byte-identical current files are counted and shown as duplicates
- [x] Both measures are sampled and exported as gauges

## Notes

**agent:claude-code/d8436f9f** at 2026-09-28T23:55:07Z

### Built

- `catalog.ArtifactBytes` returns an `ArtifactUse` with four measures from
  one read transaction: every row, each digest once, each path's current
  version (`current = 1`), and each current digest once.
- The sampler records `artifacts.current` and `artifacts.current_unique`.
  `/metrics` exports `lampi_artifact_current_bytes` and
  `lampi_artifact_current_unique_bytes`.
- The card is now "Compression against raw": current bytes ÷ the CAS's
  disk use, with raw and on-disk sizes, and duplicate files, meaning current
  rows whose digest another current row shares.
- A sample from before this change has neither measure, so the card shows
  "Not measured yet" until the next hourly sample. It does not fall back to
  the old ratio.

### Alternatives

- Keep the old ratio and relabel it. Rejected: each continuation of a
  session is a row, so the number measures how often sessions were
  resumed, not any saving.
- Raw as `MAX(size)` per path. Rejected: the `current` flag already names
  each path's head, and a file rewritten shorter would be counted at its
  old size.
- Duplicates across all versions. Rejected: an old version matching
  another path's file is rare and says nothing about what the machines
  hold now.

The live lake's catalog on the dev host is readable only by its service
user, so the PR carries no before-numbers.

**agent:claude-code/d8436f9f** at 2026-09-29T00:30:02Z

terva-review on #117, head 7dce131, run f0f233f7: clean, no findings at the failure threshold. CI green. Waiting for the owner to merge.

## Summary

Landed in #117. The operations page's storage card is now Compression against raw: the current version of every path, which is what the machines hold, divided by the stored blobs' disk use. Byte-identical current files are counted as duplicates. The sampler records artifacts.current and artifacts.current_unique, and /metrics exports both. Samples from before the change show no ratio.
