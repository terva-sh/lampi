---
schema: 3
id: TKT-01M3F2PGA1EFCEPEBPT1JFR3JJ
title: "Web retrieval: browse, search and export stored sessions"
type: epic
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
  - area/server
assignees: []
milestone: null
parent: null
origin: null
dependencies:
  - TKT-01M3F2FSTF28GNEDGQ0XBSZ44W
blocks_on: children
references:
  - ref: plan:web-ui
    path: docs/web-ui-plan.md
claim: null
archive: null
created_at: 2026-09-26T14:42:51Z
updated_at: 2026-09-26T14:42:51Z
created_by:
  id: agent:codex/web-ui-planning
  name: ""
updated_by:
  id: agent:codex/web-ui-planning
  name: ""
extensions: {}
---

## Description

### Outcome and rationale

Release B of docs/web-ui-plan.md adds transcript viewing, literal text search and selected JSONL retrieval after the OIDC dashboard. The first release remains metadata-only; this epic keeps content retrieval and its policy independent. Use the existing normalized events and projection logic, a rebuildable FTS5 index and bounded synchronous exports. Arbitrary SQL, raw CAS/Parquet downloads, large persistent export jobs and project-level viewer tenancy are excluded.

### Execution contract

Follow the linked design for generation pinning, index lag, limits, exporter role and export_projects default deny. Reuse sibling OIDC patterns already landed by release A. The local CLI policy and output formats remain compatible. Use isolated synthetic data; no production credentials or IdP changes. Direct children block epic completion. Newly filed work stays draft until owner promotion.

## Acceptance criteria

- [ ] Viewer can read and search current normalized content without mixing generations or serving stale/purged results.
- [ ] Exporter can download an explicit bounded selection under server-side default-deny project policy.
- [ ] CLI export compatibility, training redaction, raw provenance and integrated browser validation are preserved.
