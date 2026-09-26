---
schema: 3
id: TKT-01M3F2K2B7QW5SJZ9F8G65RBN5
title: "Web: validate OIDC dashboard and document hosted operation"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/ci
  - area/ops
  - area/docs
assignees: []
milestone: null
parent: TKT-01M3F2FSTF28GNEDGQ0XBSZ44W
origin: null
dependencies:
  - TKT-01M3F2K27WTA90MB3K2M2AVZ6H
blocks_on: none
references:
  - ref: plan:web-ui
    path: docs/web-ui-plan.md
claim:
  actor: agent:codex/web-ui-release-a
  branch: t3code/web-session-lake-ui
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-8b6762d2
  commit: d91b804a7317664cc25dfba5f934a6e930882442
  session: null
  claimed_at: 2026-09-26T15:18:54Z
  expires_at: null
archive: null
created_at: 2026-09-26T14:40:59Z
updated_at: 2026-09-26T15:26:12Z
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

Close release A with reproducible end-to-end proof and complete operator instructions. Validate a synthetic lake and fake HTTPS IdP, not production identities or transcripts. This ticket does not deploy or provision an IdP; actual origin/issuer/client/group/secret paths come from deployment configuration. Keep existing go-live gates separate.

### Contract and references

Follow docs/web-ui-plan.md, including pinned sibling sources and release A defaults. Use isolated synthetic data. Newly filed work stays draft pending promotion.

## Acceptance criteria

- [ ] Full fake-IdP flow and denied/expired/logout cases pass against the integrated server while agent ingestion remains functional.
- [ ] 20k-session evidence records indexed bounded reads and responsive paging under concurrent ingest, without scanning all transcript files.
- [ ] Browser smoke covers navigation, filters, refresh, empty/error states, keyboard and mobile layout with reproducible commands.
- [ ] make ci and go test -race ./... pass; any new CI gate is consistent across local tasks and both forges.
- [ ] Operator docs distinguish implemented behavior, deployment inputs and existing go-live gates; no live secret, endpoint or provisioning is required.

## Definition of done

- [ ] Focused tests pass and behavior/contracts are documented; record validation and rationale in the ticket.

## Implementation plan

Add a documented smoke fixture/harness that drives the full login flow and exercises overview, filters, metadata, conflicts, expired sessions, denied groups and logout. Include a 20k-session scenario and concurrent ingest; record query/page sizes and timings without hardware-specific pass thresholds. Run make ci and go test -race ./..., updating both workflows/Makefile/justfile only if new gates require it. Perform browser keyboard/mobile smoke with an available supported harness and record evidence. Update README, architecture, protocol, deploy examples and VPS bringup for web config, IdP registration/callback, group claims, secret permissions, TLS, session lifetime/restart and disabling web. All examples use placeholders.

## Notes

**agent:codex/web-ui-release-a** at 2026-09-26T15:26:12Z

Release validation has exercised Chromium login, mapped/denied groups, 123-session pagination, filters, metadata/provenance/conflicts, hidden-tab polling, refresh failure/recovery/session expiry, mobile/keyboard, logout, no-JS and empty-lake states. Screenshots inspected. Full make ci and go test -race ./... passed before final review fixes. Review found and corrected two compatibility/security edges: catch-all web routing changed reserved-route 405 responses, and JSON null inside a groups array could be decoded as an empty string while retaining another grant. Added regression tests and strict malformed-query refusal. Added fixed auth-refusal log reasons without provider bodies/credentials. Concurrent synthetic tests show 30 reads plus 30 ingests against 20k sessions in ~68 ms; full browser reads coexist with actual device blob/manifest requests and all ten new sessions normalize ready. Operator docs and systemd opt-in setting are now written. Final full gates and browser smoke are being rerun against these final changes.
