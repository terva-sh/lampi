---
schema: 3
id: TKT-01M3558DAGH6Z9MFG6GJ1TF0CS
title: Implement fsnotify/poll watcher for terva JSONL
type: task
status: ready
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
updated_at: 2026-09-22T18:15:12Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: human:sothr
  name: Drew Short
extensions: {}
---

## Description

Replace watch stub. Prefer fsnotify on $TERVA_HOME/sessions/**/*.jsonl; poll fallback. Debounce; track byte offset + mtime + size. Optional *.errors.jsonl.

## Acceptance criteria

- [ ] Detects append without full re-read
- [ ] Survives editor atomic replace / truncate
- [ ] Discovers swarm/subagent session files via directory walk
