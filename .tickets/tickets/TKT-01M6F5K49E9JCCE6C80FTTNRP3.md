---
schema: 3
id: TKT-01M6F5K49E9JCCE6C80FTTNRP3
title: "MCP: preview session selections and bay migration plans"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/search
  - area/auth
assignees: []
milestone: null
parent: TKT-01M6F5K38C9N9Y1TRX4PZB7JSW
origin: null
dependencies:
  - TKT-01M6F5K45WCQKE37JN4CC0WEY7
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-11-02T22:20:32Z
updated_at: 2026-11-02T22:57:44Z
created_by:
  id: agent:codex/t3-6387cc8a
  name: ""
updated_by:
  id: agent:codex/t3-6387cc8a
  name: ""
extensions: {}
---

## Description

Let an agent turn discovered sessions and doctor recommendations into a reviewable bay plan through read-only MCP tools, using the shared selection/migration-plan service. The current search interface returns event hits; it cannot create an exact session cohort or explain proposed membership changes.

Expose authorized structured selectors and exact UID sets, include/exclude refinement, matching reasons, current visible membership and proposed add/move results. Return bounded pages and a stable plan reference or portable machine-readable artifact with the state needed for an authorized operator's later apply. Specify lifecycle/expiry and avoid embedding transcript text or credentials. Support one or more proposed destinations and explain when a destination does not yet exist, needs operator creation or is outside the caller's visible scope. Scope counts and explanations before emitting them.

The agent can help identify sessions and propose a plan; events:read remains read-only. No MCP membership/grant/profile write tool is added here. A host operator applying a supplied plan must revalidate session membership, targets, holds and their own authority using the migration service. Treat transcript text as evidence, not instructions. Document the handoff to CLI/dashboard, errors, limitations and scope changes, and test cross-scope references and stale previews. An artifact must not be treated as trusted merely because an agent generated it.

## Acceptance criteria

- [ ] MCP can refine exact session cohorts and preview add/move operations to one or more destinations using the shared service.
- [ ] Plan results provide reasons, no-ops, limitations, paging and reproducible state, without hidden metadata or transcript dumps.
- [ ] The documented operator handoff revalidates state/authority and conveys no write permission through a read token or plan reference.
- [ ] Read-only annotations, audit behavior, invalid/stale references and scope changes have focused tests and API documentation.
