---
schema: 3
id: TKT-01M3558D8WN5HTVPM4KRQQCSHP
title: Define project allowlist for off-box raw
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area/agent
  - area/redact
  - phase/0-policy
  - phase/1-mvp
assignees: []
milestone: mvp
parent: TKT-01M3558D72YST7VYN39EFK272P
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:11Z
updated_at: 2026-09-22T20:21:17Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: human:sothr
  name: Drew Short
extensions: {}
---

## Description

Config surface (cwd prefix, git remote, or terva CWDHash) gating which projects may upload raw. Default deny outside allowlist. Unblocks redaction upload path.

## Acceptance criteria

- [x] Allowlist/denylist in agent config
- [x] Sync refuses non-allowlisted raw with clear error
- [x] Documented in README/docs

## Implementation plan

Add projects.allow and projects.deny to the agent config file. A rule matches on cwd prefix (path boundary), git remote (normalized), and/or terva CWDHash, and every field set on a rule must match. Deny wins. An empty allow list denies every project.

terva manifests record origin's URL and HEAD when the session cwd has a .git, so a remote rule can match. upload.Sync checks the gate before any byte is PUT and returns an error that names the session and the cwd. README and docs/architecture.md describe the config and the default deny.

## Summary

config.json projects.allow and projects.deny gate off-box raw. A rule matches a cwd prefix on a path boundary, a normalized git remote, and/or a terva cwd hash, and every set field has to match. Deny wins. An empty allow list refuses every project.

upload.Sync returns an error that names the session, cwd, cwd hash, and git remote, and it does not PUT those bytes. README and docs/architecture.md describe the file. Origin's URL is copied onto the manifest when the session cwd has a .git, which is what a remote rule matches.
