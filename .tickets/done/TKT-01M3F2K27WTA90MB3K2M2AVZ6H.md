---
schema: 3
id: TKT-01M3F2K27WTA90MB3K2M2AVZ6H
title: "Web UI: build the lake overview and metadata browser"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
assignees: []
milestone: null
parent: TKT-01M3F2FSTF28GNEDGQ0XBSZ44W
origin: null
dependencies:
  - TKT-01M3F2K241CAKSX5QM5NGP24RW
blocks_on: none
references:
  - ref: plan:web-ui
    path: docs/web-ui-plan.md
claim: null
archive: null
created_at: 2026-09-26T14:40:59Z
updated_at: 2026-09-26T15:18:54Z
created_by:
  id: agent:codex/web-ui-planning
  name: ""
updated_by:
  id: agent:codex/web-ui-release-a
  name: ""
extensions: {}
---

## Description

### Scope and rationale

Implement the first user-visible dashboard with embedded Go templates, CSS and modest JavaScript. Provide overview counts and accessible harness chart, filtered/paginated sessions, metadata details and paginated conflicts. Every data view requires viewer. No transcript, download, merge, purge or ingest controls in release A.

### Contract and references

Follow docs/web-ui-plan.md, including pinned sibling sources and release A defaults. Use isolated synthetic data. Newly filed work stays draft pending promotion.

## Acceptance criteria

- [x] Signed-in viewer can navigate overview, filtered sessions, metadata and conflicts; empty and unknown-data states are explicit.
- [x] Chart includes equivalent text counts, tables/forms work without JS, and keyboard/mobile layouts remain usable.
- [x] Polling follows visibility/page/auth rules, never extends hard session expiry, and reports stale data honestly.
- [x] Malicious names/paths/error text render as text under CSP; no external assets or frontend build is required.
- [x] Counts and labels do not imply online machines, unique CAS bytes, historical throughput or successful normalization without evidence.

## Definition of done

- [x] Focused tests pass and behavior/contracts are documented; record validation and rationale in the ticket.

## Implementation plan

Use internal/web with html/template and embedded assets, reusing read models rather than fetching the device API from the browser. Render usable tables/forms without JavaScript. Add visible-tab polling every 25 seconds for overview and first page only, manual refresh, last-success timestamp and stale/error state. Stop polling on hidden tabs or auth failures. Preserve filters and later-page reading position. Include login unavailable/denied pages and POST logout. Escape all metadata and use a CSP with self-hosted external scripts. Test renderer and polling state behavior with synthetic fixtures; document desktop/mobile keyboard smoke steps.

## Summary

Implemented embedded Go templates/CSS/JS for overview, harness counts, normalization, session filters/pagination, metadata/artifacts/provenance and conflicts. Tables/forms work without JavaScript; polling runs every 25 seconds only on visible overview/first pages, pauses on errors/auth loss/keyboard focus, and preserves later pages. Malicious metadata is escaped under CSP. Chromium/Playwright smoke passed real fake-HTTPS-IdP login, 123-session pagination/filtering, details, conflicts, visibility/error/recovery/expiry refresh, desktop/mobile layout, keyboard access, logout, no-JS, denied groups and empty lake. Smoke caught select-label ambiguity; explicit accessible names fixed it. Screenshots inspected at desktop and 390px mobile. make ci and full go test -race ./... passed before the label-only correction; focused web tests passed. The reproducible fixture/script use temp lakes and no production credentials or Node build for the app.
