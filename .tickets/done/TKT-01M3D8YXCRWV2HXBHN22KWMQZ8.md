---
schema: 3
id: TKT-01M3D8YXCRWV2HXBHN22KWMQZ8
title: "Go-live: canary secrets in three places never reach the lake"
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area/ops
  - area/redact
assignees: []
milestone: null
parent: TKT-01M3B35JS8J2F83FG4J7ZC0199
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-25T21:53:49Z
updated_at: 2026-09-26T16:29:56Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:codex/rollout
  name: ""
extensions: {}
---

## Description

Part of the go-live check in TKT-01M3B35J (Pre-deploy hardening pass). That check was written for a lake on the VPS behind Caddy. As of 2026-09-26 the dogfood lake runs on the owner's workstation, bound to loopback and serving a device token, and no other machine points at it. So a check that needs no proxy or VM runs against a dev lake from `just dev-serve`, and a check that does need one waits for the VPS bring-up.

Canary secrets. Plant three canaries and sync them to a dev lake: one token in JSON-escaped transcript text, one in a git remote URL, and one in a project that the allowlist denies. None may reach the lake. Check the CAS bytes, the catalog, and the normalized events, not only the client's output. Use obviously fake canaries in the redaction ruleset's shapes, never a real credential.

## Acceptance criteria

- [x] A canary in escaped JSON, one in a git remote, and one in a denied project are each absent from the CAS, the catalog, and the normalized events

## Implementation plan

Add a reproducible isolated go-live test using real sync and a loopback lake. Plant fake token-shaped canaries in escaped transcript text, git remote credentials and a denied project, plus a clean control. Assert expected accepted/refused sessions and scan CAS, SQLite catalog including sidecars, and normalized files for both literal and escaped canaries. No real harness homes or credentials are read.

## Summary

Passed TestGoLiveCanaries with the golive build tag: real CLI sync to an isolated HTTP lake quarantines the Unicode-escaped fake token and refuses the denied project. Clean and sanitized-remote controls are accepted and normalized. Scanned both CAS objects, catalog plus WAL/SHM, and both normalized files: no literal or escaped canary present. Explicit harness roots and a closed environment avoid live data. Reproduce: mise exec -- go test -tags golive ./internal/cli -run ^TestGoLiveCanaries$ -count=1 -v.
