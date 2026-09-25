---
schema: 3
id: TKT-01M3D779QD7C2FW0MMBDG6WJM5
title: Isolate development runs from a live lake and agent on the same machine
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
  - area/docs
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-25T21:23:27Z
updated_at: 2026-09-25T21:23:27Z
created_by:
  id: agent:claude-code/opus
  name: ""
updated_by:
  id: agent:claude-code/opus
  name: ""
extensions: {}
---

## Description

On a machine that also runs a live lake and agent, a development build run with no flags acts on live state. That already happens on the owner's workstation, where lampi is dogfooded while it is developed.

- `serve` with no `--data` writes to the XDG state dir `terva-lampi/`. That is the agent's own state directory, not an empty lake.
- `serve` with no `--addr` tries `127.0.0.1:8787`, which the live lake holds.
- `sync`, `status`, and `agent` read the XDG config dir `terva-lampi/`. They pick up the real device token, machine id, server URL, and allowlist, so a development `sync` uploads to the live lake.

The workaround today is a fresh `XDG_CONFIG_HOME` and `XDG_STATE_HOME`, plus `serve --data DIR --addr 127.0.0.1:18787`. It was verified on 2026-09-25: the throwaway lake got its own machine id and token path, refused every real session, and stored nothing.

### Ask

Add a `just dev` or `make dev` recipe, or a `--dev` convention, that sets all four in one step, and document it in the README quickstart beside `make build`. Consider a warning when `serve` runs with a default `--data` that also holds agent state (`agent.pid`, `watermarks.db`).
