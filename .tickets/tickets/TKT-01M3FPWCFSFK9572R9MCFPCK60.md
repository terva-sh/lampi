---
schema: 3
id: TKT-01M3FPWCFSFK9572R9MCFPCK60
title: "MCP: serve recall tools over the shared query layer"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
  - area/server
assignees: []
milestone: null
parent: TKT-01M3FPP3H592T31Y2M3N347CPB
origin: null
dependencies:
  - TKT-01M3F2PGMZKTFXSX521T07A4HA
  - TKT-01M3FPWCH4GFYYX4GKT9XFN53G
  - TKT-01M3FPWC9E15XG1GFS7886Z415
  - TKT-01M3FPWCBK7WQSRF723RJFXKXE
  - TKT-01M3FPWCDFXXCHD8F5PA0GGMWP
blocks_on: none
references: []
claim:
  actor: agent:claude-code/9078ac3f
  branch: t3code/expose-session-lake-tools
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-268e1246
  commit: 478435f193ed03f0e6e0e2fdf759e78bdab922a8
  session: null
  claimed_at: 2026-10-04T18:16:20Z
  expires_at: null
archive: null
created_at: 2026-09-26T20:35:36Z
updated_at: 2026-10-04T18:16:20Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/9078ac3f
  name: ""
extensions: {}
---

## Description

Serve an MCP endpoint from the lake exposing search, structured filters, event windows around a hit, deep links and copy-out as tools. Tools call the shared recall layer and return its result shapes unchanged. This replaces terva-ext-session-search for agents on the owner's machines. Blocked on the MCP auth decision (sibling ticket); prompt injection handling is out of scope per the epic.

## Acceptance criteria

- [ ] MCP tools return the same results as the web API for the same inputs.
- [ ] Documentation shows how to configure an agent to use the lake for recall instead of terva-ext-session-search.

## Notes

**agent:claude-code/dae09bda** at 2026-10-01T06:49:12Z

MCP authenticates with read tokens holding events:read, per TKT-01M3FPWCH4GFYYX4GKT9XFN53G. Tools should reuse the internal/recall event filter from TKT-01M3V3J8VZKAZJJAR9VTMDGJGD.
