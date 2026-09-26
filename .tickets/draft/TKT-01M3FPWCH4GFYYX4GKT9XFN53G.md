---
schema: 3
id: TKT-01M3FPWCH4GFYYX4GKT9XFN53G
title: "MCP: authenticate clients as OIDC users"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/auth
  - area/server
  - question
assignees: []
milestone: null
parent: TKT-01M3FPP3H592T31Y2M3N347CPB
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-26T20:35:36Z
updated_at: 2026-09-26T20:35:36Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

Decide and implement how an MCP client authenticates: extend OIDC to MCP clients, or let a signed-in user generate revocable bearer tokens that act as them. Either way the credential maps to an OIDC identity and its roles, is revocable, and is never a device token. Needs an owner decision before implementation.

## Acceptance criteria

- [ ] The chosen mechanism is recorded with the rejected alternative and why.
- [ ] MCP requests authenticate as a user identity with that user's roles; device tokens and browser cookies do not authorize MCP.
