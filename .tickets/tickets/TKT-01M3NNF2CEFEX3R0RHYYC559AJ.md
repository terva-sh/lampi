---
schema: 3
id: TKT-01M3NNF2CEFEX3R0RHYYC559AJ
title: "Bays: agent bay requests and terva-lampi bays"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/agent
assignees: []
milestone: null
parent: TKT-01M3N8KHW56GVZXHV0KBNSPEX1
origin: null
dependencies:
  - TKT-01M3NNF29WCV5F1D8QSBWPM6M0
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T04:06:17Z
updated_at: 2026-09-29T14:58:27Z
created_by:
  id: agent:claude-code/7859b064
  name: ""
updated_by:
  id: agent:claude-code/7859b064
  name: ""
extensions: {}
---

## Description

Agent side of bays. Design in the parent epic TKT-01M3N8KHW5 (Bays: segment one lake and route sessions to a bay).

### Scope

- Each lake's entry in `config.json` gains bay request rules using the same matcher as `projects.allow` (`cwd_prefix`, `cwd_glob`, `git_remote`, `git_remote_prefix`, plus harness, naming one or more bays) and `default_bays`. A lake profile may suggest both. Local config wins, as it does for other profile fields.
- The agent sends the requested bays in the manifest and announces bay support.
- `terva-lampi bays` lists the bays each lake lets this device write to. `terva-lampi bays which [PATH]` says which lake and bays a session started at PATH would request and which rule caused each.
- A request for a bay not in the device's writable list warns in `status`, and the request is still sent so the lake records it.
- A session the lake refuses as "no bay" stays pending and is listed as such in `status`, and reported to the lake so the device view shows it.
- When per-device overrides land (TKT-01M3N22HC), bay requests can be set there too. Not required for this ticket.
- Documented in `docs/registration-and-lakes.md` and `docs/allowlist-and-redaction.md`.

## Acceptance criteria

- [ ] Bay request rules and default_bays route one session to two bays
- [ ] terva-lampi bays which PATH names the lake, bays and deciding rule
- [ ] A no-bay refusal keeps the session pending and shows in status
