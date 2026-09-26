---
schema: 3
id: TKT-01M3F2PGRMS5NJZK90JCTAF0SP
title: "Export: add bounded authorized web downloads and shared projection"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/auth
  - area/normalize
assignees: []
milestone: null
parent: TKT-01M3F2PGA1EFCEPEBPT1JFR3JJ
origin: null
dependencies:
  - TKT-01M3F2PGDY06D7XE12NWQ9EZF4
blocks_on: none
references:
  - ref: plan:web-ui
    path: docs/web-ui-plan.md
claim: null
archive: null
created_at: 2026-09-26T14:42:52Z
updated_at: 2026-09-26T14:42:52Z
created_by:
  id: agent:codex/web-ui-planning
  name: ""
updated_by:
  id: agent:codex/web-ui-planning
  name: ""
extensions: {}
---

## Description

### Scope and rationale

Implement exporter-only POST downloads for selected session UIDs and events/sharegpt/trajectory formats. Exporter implies viewer; viewer alone cannot export. Add server web export_projects allow/deny policy using existing match semantics, default deny for all web formats. Preserve CLI behavior while extracting shared projection logic. Unlike the CLI skipping behavior, web preflight rejects the full selection if any session is forbidden, missing, not current/available or ineligible.

### Contract

Follow docs/web-ui-plan.md release B and its pinned sibling references. Use only isolated synthetic data. New work remains draft pending promotion.

## Acceptance criteria

- [ ] Only exporter-authorized, CSRF-valid explicit selections pass; every format is default-denied without a matching server project rule and deny wins.
- [ ] CLI event/training outputs and original policy behavior remain compatible; training redaction/provenance/encryption semantics are preserved.
- [ ] Unavailable/forbidden/ineligible selections fail preflight as a whole; mixed generations and silent skipping are prevented.
- [ ] Count/byte/concurrency/time limits and private temporary-file cleanup hold on success, error, cancellation and restart.
- [ ] Downloads are safe attachments; audits identify actor/selection/outcome without content, codes, tokens or secrets.

## Definition of done

- [ ] Focused tests and relevant API/operator docs are complete; record evidence and decisions in the ticket.

## Implementation plan

Read internal/cli/export.go and internal/normalize/sharegpt.go. Extract reusable reader/projection functions without CLI output changes. Extend role config and add CSRF-protected /api/web/v1/exports. Limit 100 UIDs, 64 MiB prepared output, two concurrent jobs and two minutes; reject oversized requests and bound temp disk usage. Pin current generations and prepare the entire output in private temp files before attachment headers, then stream and clean on completion/cancel/restart. Recheck authorization/policy for each request. Retain raw_sha256, training plaintext stripping and opaque encrypted data. Events get no new redaction pass. Categorized audit records include issuer/subject, selected UIDs, format, result and bytes but no content/credentials. Test cancellation, cap excess, stale generations, no-turn training sessions and malicious download names.
