---
schema: 3
id: TKT-01M39306SSJQ9WWGHFWEA840FF
title: Deploy examples for Shape A harnesses
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
parent: TKT-01M39306SFZNBM2YE2TJ49S7PP
origin: null
dependencies:
  - TKT-01M38RJCDREDTTPTY2D7SR8W59
  - TKT-01M39306SHCKX7FD61WJQB2PDZ
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-24T06:53:15Z
updated_at: 2026-09-24T08:35:45Z
created_by:
  id: agent:atlas/architect
  name: Atlas - Architect
updated_by:
  id: agent:cursor/d70d
  name: Cursor cloud agent
extensions: {}
---

## Description

Update deploy examples / config samples so operators can copy Shape A `harnesses` enable and root overrides. Keep loopback server placeholders. Do not add VPS hostname, restic, or frontend install steps.

## Acceptance criteria

- [x] An example `config.json` (or documented fragment) shows `harnesses` with at least one `enabled: false` and one `root` override
- [x] Agent unit/env examples note restart-to-reload and that env remains a debug override under the locked precedence
- [x] Examples still default to loopback lake URL and do not commit production hostnames or tokens
- [x] No restic, VPS bring-up, purge, or soft-link content is introduced
- [x] Cite the status harness line shape so operators can verify resolve source

## Definition of done

- [x] Deploy/docs examples updated and referenced from the epic
- [x] Matches schema from TKT-01M39306SHCKX7FD61WJQB2PDZ

## Implementation plan

### Samples

Add deploy/config.json.example. server stays http://127.0.0.1:8787. harnesses sets claude enabled false and codex root to an absolute placeholder. No token, no projects, no secrets inside a harness entry. Keys stay the protocol ids.

### Operator notes

deploy/README.md gets a Harnesses section: restart to reload, env is a debug override, precedence is flag (if any), then config root, then env, then the adapter default. Cite the status line from the status command:

harness <id> enabled=<true|false> root=<absolute path or empty> source=<config|env|default>

The systemd user unit, agent.env.example, and the launchd plist carry the same restart and precedence notes. They keep the loopback URL.

### Checks

A config test loads the example through LoadFile so the sample stays on the harnesses schema. The Shape A epic stays ready: Allowlist DX notes is still open. A note on the epic points at the example.

## Summary

deploy/config.json.example is the Shape A sample. server stays http://127.0.0.1:8787. harnesses.claude.enabled is false. harnesses.codex.root is the absolute placeholder /home/you/.codex. Omitted ids stay on. The file has no token and no harness allowlist fields.

deploy/README.md documents restart-to-reload, the debug env override, and the locked precedence: flag (if any), then config root, then env, then the adapter default. The systemd user unit, agent.env.example, and the launchd plist carry the same notes and still default to the loopback URL.

terva-lampi status is cited with the line from the status command:

harness <id> enabled=<true|false> root=<absolute path or empty> source=<config|env|default>

TestDeployConfigExample loads the sample through config.LoadFile. The parent epic TKT-01M39306SFZNBM2YE2TJ49S7PP (Config/DX Shape A — harness enable and root overrides) closes with this branch. TKT-01M39306SQYDYS7Y6X1KXRJXM0 (Allowlist DX notes for Cursor cwd refuses) is already done on main.
