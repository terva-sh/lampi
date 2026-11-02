---
schema: 3
id: TKT-01M6F35AC4H7S1EK6XVN9E9KAQ
title: "Release v0.11.1: review groups and profile editor redesign"
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/ci
  - area/ops
  - area/docs
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/t3-43103b81
  branch: tickets/v0.11.1
  worktree: /home/sothr/.t3/worktrees/lampi/t3-43103b81
  commit: 527d5c368339ad8fc3f26cc6b81a365047cdb4f1
  session: null
  claimed_at: 2026-11-02T21:38:05Z
  expires_at: null
archive: null
created_at: 2026-11-02T21:38:02Z
updated_at: 2026-11-02T21:38:05Z
created_by:
  id: agent:claude-code/t3-43103b81
  name: ""
updated_by:
  id: agent:claude-code/t3-43103b81
  name: ""
extensions: {}
---

## Description

Release v0.11.1 from main 527d5c3: everything merged since v0.11.0, PRs #241 to #245. The owner asked on 2026-11-02 to land these and cut v0.11.1, a patch version because the changes continue the v0.11.0 profile tools; that authorizes the tag on both forges. Upgrading the live lake is not part of this ticket.

### Scope

- #241 review queue groups by owner or folder, with a temporary-folder group that offers only Hide all (TKT-01M6ETFR3B1KT33JGS4F1W41D9).
- #242 profile editor: one line per rule, matches per rule, inline doctor findings, filter (TKT-01M6ETZV536S1Z3SP2D5KZ4GJX).
- #243 editor ordering: move, drag, sort by folder and owner, added rules land beside their group (TKT-01M6ETZV7C0P5F2RDKQPAQCCFW).
- #244 merge ticked allow rules under one prefix with a preview (TKT-01M6ETH6H726EM1DJJS98M56JV).

### Upgrade facts

No change to internal/catalog/catalog.go, internal/normalize or internal/recall/index.go since v0.11.0: schema stays 21, index 8, no renormalize. The review API adds `groups` and a per-row `temporary`; nothing removed. Agents unchanged.

### Alternatives

v0.12.0 was the other choice, since the release adds dashboard features. The owner chose a patch number because these extend the v0.11.0 features and change no format, schema or API contract; rollback to v0.11.0 is a binary swap either way.

## Acceptance criteria

- [ ] An isolated v0.11.0 upgrade rehearsal and the release gates pass on the synchronized main commit
- [ ] The tag is published on both forges, and the archives, checksums and images name the tag
- [ ] Both release bodies describe the features, the upgrade and rollback
