---
schema: 3
id: TKT-01M6F5K49E9JCCE6C80FTTNRP3
title: "MCP: preview session selections and bay migration plans"
type: task
status: done
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
updated_at: 2026-11-02T23:21:55Z
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

- [x] MCP can refine exact session cohorts and preview add/move operations to one or more destinations using the shared service.
- [x] Plan results provide reasons, no-ops, limitations, paging and reproducible state, without hidden metadata or transcript dumps.
- [x] The documented operator handoff revalidates state/authority and conveys no write permission through a read token or plan reference.
- [x] Read-only annotations, audit behavior, invalid/stale references and scope changes have focused tests and API documentation.

## Implementation plan

Expose select_sessions, preview_bay_migration and preview_bay_rule as read-only MCP tools and authenticated POST read endpoints over the shared catalog service. Accept bounded typed UID arrays/filters, exclusions, multiple targets and hypothetical names; return complete portable artifacts instead of sampled pages. Keep events:read and existing bay/session scopes, strict schema validation, no-store and durable audit. Audit counts/argument lengths rather than free-text reasons. Document 24-hour plan expiry, untrusted artifact handoff, scoped previews requiring a complete host preview, and no mutation authority. Add parity, scope, read-only, audit and stale-handoff tests.

## Notes

**agent:codex/t3-6387cc8a** at 2026-11-02T23:21:54Z

Added select_sessions, preview_bay_migration and preview_bay_rule with typed arrays/nested metadata, strict advertised keys, read-only annotations and corresponding authenticated POST read endpoints. They share catalog selection/plan services, events:read and current scopes; complete bounded artifacts fail on excess rather than sampling. No mutation tool or grant/profile/route write exists. MCP audit records counts and argument lengths rather than nested metadata/free-text reasons; HTTP audits the operation. Scoped plans hide co-memberships and policy, treat proposed names hypothetically and cannot apply until a full operator re-preview. Full-scope plans still require independent current administrator authority. Documentation covers schemas, 64 KiB request / 500-candidate / 8 MiB output bounds, 24-hour expiry, untrusted artifacts, exact operator handoff and receipt recovery. Tests verify HTTP/MCP parity (excluding per-call timestamps/checksums), scope/redaction, audit privacy, no membership/bay/rule/receipt writes, invalid inputs, absent mutation methods, complete host handoff and stale state. Full web/cli packages pass; focused MCP/preview race tests, vet and whitespace checks pass. Installed live lake has not been changed.

## Summary

Delivered three read-only MCP/HTTP selection and migration/routing preview operations, portable bounded plans and documented operator handoff. Shared scoped catalog logic and strict schemas preserve events:read as read-only; authorization, audit, parity and stale-plan tests pass.
