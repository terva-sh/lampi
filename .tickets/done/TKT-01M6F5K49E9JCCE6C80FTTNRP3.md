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
updated_at: 2026-11-02T23:55:28Z
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

**agent:codex/t3-6387cc8a** at 2026-11-02T23:48:15Z

### Accepted review finding: read reach does not establish policy authority

Review 3012, finding-1, on head 7ad15f673019ada95df7faca703040a29b224326 exposed a real authorization bug: unrestricted events:read tokens produced AllBays and could see source/destination grant principals. Review: https://git.local.sothr.com/terva-sh/lampi/pulls/253#issuecomment-37712.

PlanBayMigration now takes independently established administrator authority, matching apply. Only administrator authority plus unrestricted scope enables named readership, the lake-wide policy fingerprint and complete_memberships. All read-token HTTP/MCP previews pass false, stay advisory and require full administrator re-preview before apply. Receipt verification and inverse recovery now also require explicit administrator authority; host CLI access and guarded AdminOnly dashboard handlers are the trusted callers. Inferring admin from scope.All was rejected because it grants data reach without policy-inspection authority. No new token capability or live credential is introduced.

Catalog coverage tests unrestricted/scoped read access and scoped admin access against named source/destination sentinels, administrator re-preview enforcement, and receipt/recovery refusal without authority. HTTP/MCP regression uses an actual unrestricted events:read token, verifies generic destination warnings, no source/destination sentinel disclosure, and no apply-ready artifact. Full administrator previews still name bounded source/destination readers. Documentation and tool guidance explain the boundary.

Focused catalog/web/CLI tests and full go test -trimpath=false ./... passed, as did go vet ./..., the isolated CLI build and git diff --check. Affected-package race tests are running before push. No live setup, grants, routes or memberships were changed.

**agent:codex/t3-6387cc8a** at 2026-11-02T23:55:28Z

### Accepted review 3013 finding-1: hidden co-membership inference

Review 3013 at b5fcfa048782604289ac25aa978a4fa5bf7f4271 confirms review 3012's principal-disclosure finding is resolved, but identifies a separate medium authorization leak. Removing the last visible membership distinguished hidden co-membership through last_default_bay status or implicit default fallback deltas. Review: https://git.local.sothr.com/terva-sh/lampi/pulls/253#issuecomment-37718.

Advisory migration simulation now starts from visible memberships; all add/remove/no-op calculations use that view. Removing every visible membership reports needs_admin_review with no inferred default fallback, regardless of hidden memberships. Preserve the exact request and regenerate the full administrator preview to resolve it. Existing administrator last-default protection and named-bay fallback remain unchanged. Inferring global validity from hidden rows was rejected because the resulting public status discloses private membership existence.

The same audit found full session-state fingerprints and hidden hold counts could expose hidden changes. Advisory artifacts now omit the full state digest and consider only visible holds. Complete administrator previews retain full state fingerprints and all-hold validation for stale/atomic apply. No live state changes are involved.

Catalog regression compares complete canonical advisory artifacts, including plan ID, before and after hidden membership/hold changes, for default/named source, remove-only, no-op, addition and multiple-destination move. HTTP/MCP regressions use bay-scoped events:read tokens and compare complete responses except time-derived identity before/after hidden membership changes for both last-default and named-fallback probes. Focused catalog/web/CLI tests pass. Full normal tests, affected race checks, vet and build are being completed before the single final push and review dispatch.

## Summary

Delivered three read-only MCP/HTTP selection and migration/routing preview operations, portable bounded plans and documented operator handoff. Shared scoped catalog logic and strict schemas preserve events:read as read-only; authorization, audit, parity and stale-plan tests pass.
