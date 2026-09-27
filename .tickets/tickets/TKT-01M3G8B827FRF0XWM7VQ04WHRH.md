---
schema: 3
id: TKT-01M3G8B827FRF0XWM7VQ04WHRH
title: "Client config: lock config.json across processes for read-edit-write"
type: bug
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/agent
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-27T01:40:48Z
updated_at: 2026-09-27T20:52:53Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Nothing locks config.json across processes. `config.SetLake`, `RemoveLake`, and the `UpdateLake` added for key rotation (TKT-01M3FKS3X, review 986) each read the file, edit it and rename a new copy into place. Two writers running at once, such as `terva-lampi register --replace` beside an agent moving its pin, can lose one write in the gap between one writer's read and its rename. The window is short, and each write is otherwise atomic.

A related case: `register` saves the lake's profile cache before it writes the config entry. If `refreshPin` refuses a changed entry at the same moment and puts its old cached profile back, it can overwrite the profile `register` just saved.

Also noted during that review: `register --replace` writes to `tokens/<name>.token`. If a different lake's entry names that same file as its token, it would be overwritten. Nothing creates that setup, but a hand edit could.

Likely fix: a lock file beside config.json (flock on Unix, LockFileEx on Windows), held across read-edit-rename and the profile-cache write.
