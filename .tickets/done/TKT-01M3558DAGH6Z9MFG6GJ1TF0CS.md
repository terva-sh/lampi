---
schema: 3
id: TKT-01M3558DAGH6Z9MFG6GJ1TF0CS
title: Implement fsnotify/poll watcher for terva JSONL
type: task
status: done
status_reason: null
priority: urgent
due_on: null
labels:
  - area/agent
  - phase/1-mvp
assignees: []
milestone: mvp
parent: TKT-01M3558D9Z1ZWHXB3V14A4AJGR
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:11Z
updated_at: 2026-09-22T19:05:22Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/d943
  name: Cursor cloud agent
extensions: {}
---

## Description

Replace watch stub. Prefer fsnotify on $TERVA_HOME/sessions/**/*.jsonl; poll fallback. Debounce; track byte offset + mtime + size. Optional *.errors.jsonl.

## Acceptance criteria

- [x] Detects append without full re-read
- [x] Survives editor atomic replace / truncate
- [x] Discovers swarm/subagent session files via directory walk

## Implementation plan

Fill internal/watch. A Watcher walks $TERVA_HOME/sessions recursively (swarm and subagent JSONL at any depth, plus optional *.errors.jsonl), prefers fsnotify, and falls back to polling when fsnotify cannot be opened or ForcePoll is set.

Per path it keeps size, mtime, and inode. An append on a stable inode emits the previous size as the byte offset so the consumer reads only the tail. A shrink, an inode change, or a remove/rename followed by a new file resets the offset to zero (truncate or editor atomic replace). Events for one path are debounced. The agent command runs this watcher instead of the stub and does not upload.

## Summary

internal/watch replaces the stub. fsnotify watches $TERVA_HOME/sessions and a poll walk is the fallback. An append reports the previous size as the byte offset. An inode change, a shrink, or a remove-and-replace reports offset 0. The initial and follow-up walks find nested swarm and subagent JSONL, and *.errors.jsonl unless SkipErrors is set. terva-lampi agent runs this watcher and still does not upload.
