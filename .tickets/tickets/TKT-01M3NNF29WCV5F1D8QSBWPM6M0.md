---
schema: 3
id: TKT-01M3NNF29WCV5F1D8QSBWPM6M0
title: "Bays: manifest bays field and lake routing rules"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/protocol
  - area/server
assignees: []
milestone: null
parent: TKT-01M3N8KHW56GVZXHV0KBNSPEX1
origin: null
dependencies:
  - TKT-01M3NNF27AS0NCTG7N8XFDMWMK
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T04:06:17Z
updated_at: 2026-09-29T15:05:05Z
created_by:
  id: agent:claude-code/7859b064
  name: ""
updated_by:
  id: agent:claude-code/7859b064
  name: ""
extensions: {}
---

## Description

Protocol and lake-side routing. Design in the parent epic TKT-01M3N8KHW5 (Bays: segment one lake and route sessions to a bay).

### Scope

- Additive protocol, `capture_protocol` stays 1. The manifest gains an optional `bays` field (ids or names). The lake publishes each device's writable bays, in `hello` or on a new route, and only those. A device never learns other bay names.
- Lake rules reuse the profile rule matcher (`cwd_prefix`, `cwd_glob`, `git_remote`, `git_remote_prefix`) plus harness, with actions hold (replace the requested bays with a holding bay until released), add (also put it in a bay) and deny (keep it out of a bay). Hold wins. Rules are edited by an admin on the host CLI.
- The requested bays are always recorded per manifest. A requested bay the device may not write is refused and recorded, and places nothing. A session that its requests and the rules leave with no bay lands in `default`, or is refused as below when the default is off (review 1377 on #148).
- Every manifest is routed again, add-only. A hold that matches a session already in other bays flags it for review and does not remove it.
- With the default turned off, a session nothing places is refused with a distinct error code, sent only to agents that announce bay support. An old agent gets a plain 4xx it already backs off on.
- Release of a hold restores the recorded requested bays in one step. Routing decisions that change membership go to the audit outbox.
- A rule on a folder must not catch everything beneath it: check the state of TKT-01M3NQ83 (Allow on a folder project allows everything under it, home dirs too) before relying on the matcher.

## Acceptance criteria

- [ ] capture_protocol stays 1 and an old agent syncs unchanged into default
- [ ] hold, add and deny rules apply at ingest and on every later manifest, add-only
- [ ] A refused request lands the session in default with the refusal recorded
- [ ] With default off, an unplaced session is refused with the new code for bay-aware agents and a plain 4xx for old ones
