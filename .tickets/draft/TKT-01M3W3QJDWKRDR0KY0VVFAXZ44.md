---
schema: 3
id: TKT-01M3W3QJDWKRDR0KY0VVFAXZ44
title: "CI: lint the deploy examples so units and plists stay valid"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/ci
  - area/ops
assignees: []
milestone: null
parent: TKT-01M3W3PZSDCB5FGKJS06MSZH65
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-01T16:11:02Z
updated_at: 2026-10-01T16:11:02Z
created_by:
  id: agent:claude-code/0f3154cf
  name: ""
updated_by:
  id: agent:claude-code/0f3154cf
  name: ""
extensions: {}
---

## Description

The examples in `deploy/` are copied by hand and nothing checks them,
so a renamed flag or a typo in a unit reaches a user before anyone
notices. Add a CI job that:

- runs `systemd-analyze verify` on every unit in `deploy/systemd/`;
- checks every plist in `deploy/launchd/` is well-formed (`plutil
  -lint` where a macOS runner exists, an XML parse otherwise);
- runs `docker compose config` on `deploy/compose/`;
- checks that every flag a unit or plist passes to `terva-lampi`
  exists in that subcommand's `--help`;
- validates the example JSON files against their loaders
  (`config.json.example`, `web-config.json.example`,
  `profiles.json.example`).

## Acceptance criteria

- [ ] CI verifies every systemd unit, plist and compose file in deploy/
- [ ] CI fails when a unit passes a flag terva-lampi does not have
- [ ] The example JSON files load through their real loaders in CI
