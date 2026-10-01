---
schema: 3
id: TKT-01M3W3QHV2GF2MT757H4DX1C8S
title: "Deploy: serve as a systemd user unit and a launchd agent"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
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

A laptop lake needs `serve` running as the user, on loopback, with
its data in the user's state directory. The tree has no example for
that:

- `deploy/systemd/terva-lampi-serve.service` is a system unit: it
  runs as a `terva-lampi` user, reads `/var/lib/terva-lampi`, and
  expects a token file.
- `deploy/launchd/` holds only `sh.terva.lampi.agent.plist`.

Add a systemd user unit and a launchd agent plist for `serve` that
bind `127.0.0.1:8787`, pass no token file, and keep the default data
directory. Order the agent after the lake where the service manager
can express it (`After=`/`Wants=` in the user unit; launchd cannot, so
say that the agent retries). Keep the sandboxing from the system unit
that still applies to a user unit. Document both in deploy/README.md
next to the agent examples, with the commands to enable, check and
read logs from each.

## Acceptance criteria

- [ ] A systemd user unit runs serve on loopback with the default data directory
- [ ] A launchd plist does the same on macOS
- [ ] deploy/README.md documents enabling, checking and reading logs for both
