---
schema: 3
id: TKT-01M3D779QD7C2FW0MMBDG6WJM5
title: Isolate development runs from a live lake and agent on the same machine
type: task
status: done
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
updated_at: 2026-09-25T21:35:36Z
created_by:
  id: agent:claude-code/opus
  name: ""
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
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

## Summary

The `just dev`, `just dev-serve` and `just dev-clean` recipes are in, with `make dev ARGS=...`, `make dev-serve` and `make dev-clean` for parity. They set `XDG_CONFIG_HOME` and `XDG_STATE_HOME` under `.dev/` in the checkout. They set `LAMPI_SERVER` to the dev address and set `LAMPI_TOKEN_FILE` empty, which clears an inherited value. `serve` gets `--data .dev/lake` and `--addr` from `LAMPI_DEV_ADDR`, which defaults to `127.0.0.1:18787`. `.dev/` is gitignored, and the README quickstart documents the recipes.

Verified on the owner's workstation with the live lake on `:8787` and the live agent running. The dev run got its own machine id and a token path under `.dev`. It reported `server source=env` at `:18787`. `sync` refused every real session and stored nothing. The live lake kept answering, and a fingerprint of `~/.config/terva-lampi` and the list of state files matched before and after.

### Decisions

- The state lives in the checkout's `.dev/`, not in a temp directory, so the machine id and the dev lake persist between runs, and each worktree gets its own. Two worktrees still share the default port. `LAMPI_DEV_ADDR` handles that.
- Each recipe runs the `build` recipe first, so a dev run never uses a stale binary. The Go cache makes that cheap.
- No warning was added to `serve` for a default `--data` that holds agent state. The README quickstart runs `serve` and `sync` with defaults, which puts the lake and the agent state in one directory on purpose, so the warning would fire for the documented setup. The recipes remove the reason a developer would hit it.
- `XDG_CONFIG_HOME` also moves the default Cursor IDE and Cursor CLI roots. That is documented rather than worked around, because pointing a dev run at real Cursor state should be a deliberate `harnesses` entry.
