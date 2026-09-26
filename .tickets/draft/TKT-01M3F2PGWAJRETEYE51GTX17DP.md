---
schema: 3
id: TKT-01M3F2PGWAJRETEYE51GTX17DP
title: "Web retrieval: integrate downloads and validate the release"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/ci
  - area/docs
assignees: []
milestone: null
parent: TKT-01M3F2PGA1EFCEPEBPT1JFR3JJ
origin: null
dependencies:
  - TKT-01M3F2PGMZKTFXSX521T07A4HA
  - TKT-01M3F2PGRMS5NJZK90JCTAF0SP
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

Finish release B with exporter download controls on session/search selection views, visible policy/preflight errors and end-to-end evidence. Users choose explicit session UIDs and format; viewer sees content but cannot invoke download. Explain that events contain normalized text without an additional stripping pass while training formats strip configured plaintext matches. No general SQL console, raw downloads or async jobs.

### Contract

Follow docs/web-ui-plan.md release B and its pinned sibling references. Use only isolated synthetic data. New work remains draft pending promotion.

## Acceptance criteria

- [ ] Exporter can download an explicit selection from detail/search; viewer cannot, and every preflight failure is understandable without an empty successful download.
- [ ] Events versus training behavior and all download limits are visible; no raw data/SQL route is introduced.
- [ ] Integrated tests cover viewer/exporter separation, deny policy, generation races, index lag, purge and cross-harness fixture output.
- [ ] Browser smoke and make ci/go test -race ./... pass with synthetic data and fake IdP; documentation describes recovery and operation.

## Definition of done

- [ ] Focused tests and relevant API/operator docs are complete; record evidence and decisions in the ticket.

## Implementation plan

Integrate POST export using same-origin CSRF protection and a browser download flow that handles JSON preflight errors before saving an attachment. Show selection count, format and limits; disable export for viewer and unavailable selections without relying on UI enforcement. Test a fake-IdP viewer/exporter against synthetic multi-harness content, search lag, append/renormalize, purged sessions, forbidden projects and no-training-turn cases. Compare permitted output to CLI fixtures. Run make ci and go test -race ./..., perform documented browser smoke, and update architecture/browser API/deployment docs for exporter mapping, explicit server export policy, FTS rebuild/backup and limits.
