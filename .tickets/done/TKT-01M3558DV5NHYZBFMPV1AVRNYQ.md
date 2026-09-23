---
schema: 3
id: TKT-01M3558DV5NHYZBFMPV1AVRNYQ
title: Phase 2 multi-harness JSONL peers
type: epic
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/adapter
  - phase/2-harness
assignees: []
milestone: phase-2
parent: null
origin: null
dependencies:
  - TKT-01M3558DP1WFP9WNHEP6BDVGN3
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:11Z
updated_at: 2026-09-23T16:52:56Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/2ecc
  name: Cursor cloud agent
extensions: {}
---

## Description

Claude Code + Codex adapters, git-remote project linking (Layer C), async normalizer/parquet, optional hook nudge. Depends on MVP gate.

## Definition of done

- [x] All children of this epic are done or explicitly deferred
- [x] TKT-01M3558DVTRM3ZFEYXNH191KES Claude Code JSONL adapter
- [x] TKT-01M3558DWF1399D7DNZ0FBSZ6F Codex CLI rollout JSONL adapter
- [x] TKT-01M3558DX60C92B1YRTT4M1Y3X Project linking via normalized git remote
- [x] TKT-01M3558DXV7FRPJ5A5HXMSW5B4 Async normalizer workers + parquet partitions
- [x] TKT-01M3558DYH3FVT06XBPKWAAMKH Optional terva hook nudge (shipped example)

## Notes

**agent:cursor/e4d5** at 2026-09-23T12:27:00Z

Promoted with its five children. TKT-01M3558DP1WFP9WNHEP6BDVGN3 (MVP acceptance, status, ops) is done, which was the dependency. Phase 3, Phase 4, and Phase 5 stay draft.

**agent:cursor/2ecc** at 2026-09-23T16:52:32Z

The last open child, TKT-01M3558DYH3FVT06XBPKWAAMKH (Optional terva hook nudge (shipped example)), is done. The other four children were already done. Every definition-of-done line is checked.

## Summary

Phase 2 is done. Claude Code and Codex JSONL are discovered and uploaded, projects link by normalized git remote and root commit, normalize runs off the manifest ACK into JSONL and hive-partitioned parquet, and the terva post_tool_use hook is a supported optional acceleration with the filesystem watch still the source of truth.
