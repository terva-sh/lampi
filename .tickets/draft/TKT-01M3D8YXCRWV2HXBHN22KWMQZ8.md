---
schema: 3
id: TKT-01M3D8YXCRWV2HXBHN22KWMQZ8
title: "Go-live: canary secrets in three places never reach the lake"
type: task
status: draft
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
updated_at: 2026-09-25T21:53:49Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

Part of the go-live check in TKT-01M3B35J (Pre-deploy hardening pass). That check was written for a lake on the VPS behind Caddy. As of 2026-09-26 the dogfood lake runs on the owner's workstation, bound to loopback and serving a device token, and no other machine points at it. So a check that needs no proxy or VM runs against a dev lake from `just dev-serve`, and a check that does need one waits for the VPS bring-up.

Canary secrets. Plant three canaries and sync them to a dev lake: one token in JSON-escaped transcript text, one in a git remote URL, and one in a project that the allowlist denies. None may reach the lake. Check the CAS bytes, the catalog, and the normalized events, not only the client's output. Use obviously fake canaries in the redaction ruleset's shapes, never a real credential.

## Acceptance criteria

- [ ] A canary in escaped JSON, one in a git remote, and one in a denied project are each absent from the CAS, the catalog, and the normalized events
