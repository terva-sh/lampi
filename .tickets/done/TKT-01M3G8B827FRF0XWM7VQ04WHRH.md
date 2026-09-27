---
schema: 3
id: TKT-01M3G8B827FRF0XWM7VQ04WHRH
title: "Client config: lock config.json across processes for read-edit-write"
type: bug
status: done
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
updated_at: 2026-09-27T22:53:14Z
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

## Implementation plan

A lock file beside config.json (config.json.lock), flock on unix and LockFileEx on windows, from a new internal/filelock package that identity now uses too. config.Locked(getenv, fn(Tx)) holds it; SetLake, UpdateLake and RemoveLake take it, and Tx has the same edits plus Lake(name) for callers already holding it, so nothing locks twice (flock between two descriptors in one process blocks). Profile cache writes that must stay in step with the entry go inside the lock: register saves the profile and writes the entry in one Locked; refreshPin fetches outside the lock, then under it checks the entry, saves the profile and moves the pin, restoring the old cache only if the pin write fails; the agent's hourly profile save checks the entry still pins what it fetched under. On a platform with no file lock the writes run unlocked, as before; identity keeps refusing there.

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-27T22:53:14Z

Alternatives: an in-process mutex alone would not cover register beside the agent (two processes). A reentrant lock keyed by path cannot tell goroutines apart, so an agent's concurrent lakes would share it; the Tx handle avoids reentrancy instead. Holding the lock across refreshPin's network fetch would block register for up to the fetch timeout, so the fetch stays outside and the check moves inside. The ticket's third point (register --replace overwriting a token file another lake names) was already handled by tokenSharers in register.go, which refuses. TestConcurrentLakeWritesAreNotLost fails without the lock (1 of 24 lakes survived) and passes with it.

## Summary

config.json writes are serialized across processes by config.json.lock (internal/filelock: flock / LockFileEx). register writes its profile cache and entry under the lock; refreshPin checks the entry, saves the profile and moves the pin under it and leaves the cache untouched when the entry changed, so it can no longer put an old profile over one register just saved; the agent saves a fetched profile only while the entry still pins what it fetched under. Tests: concurrent SetLake loses nothing, Tx edits without relocking, refreshPin leaves the cache file in place on a changed entry, and the agent save refuses a stale entry. Docs: registration-and-lakes.md.
