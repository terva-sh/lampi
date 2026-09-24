---
schema: 3
id: TKT-01M39306SFZNBM2YE2TJ49S7PP
title: Config/DX Shape A — harness enable and root overrides
type: epic
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
dependencies:
  - TKT-01M38RJCDREDTTPTY2D7SR8W59
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-24T06:53:15Z
updated_at: 2026-09-24T08:36:02Z
created_by:
  id: agent:atlas/architect
  name: Atlas - Architect
updated_by:
  id: agent:cursor/d70d
  name: Cursor cloud agent
extensions: {}
---

## Description

Operator config/DX for harness capture after the normalize epic. Drew locked Shape A: an optional `harnesses` map in client `config.json` with per-harness `enabled` and optional absolute `root`. Env stays a debug override. Project allowlist and secrets stay out of harness blocks.

This epic does not implement normalize projectors, VPS bring-up, restic, AgentsView/deja-vu, enrolment, purge tooling, or soft-link wiring.

### Locked decisions

- Precedence for harness root: flag (if any) > config `root` > env > adapter default
- Missing or omitted `harnesses` key = today's behavior (all wired harnesses use current Home()/env resolution)
- Unknown harness key in `harnesses` = config load error
- `enabled: false` skips discover, watch, and upload for that harness only; watermarks and CAS are untouched
- No allowlist rules and no secrets inside harness blocks
- Restart-to-reload is OK for v1 (same as server/token/allowlist today)

### Known harness keys (must match protocol harness ids)

`terva`, `claude`, `codex`, `opencode`, `cursor`, `cursor-cli`

### Queue

Blocked by normalize epic TKT-01M38RJCDREDTTPTY2D7SR8W59 (Cursor IDE then Cursor CLI children). Do not start children until that epic is done.

### Samples

deploy/config.json.example is the copy-paste client file. It sets Claude `enabled` false, sets a Codex `root` to an absolute placeholder, and leaves `server` at `http://127.0.0.1:8787`. deploy/README.md, the systemd user unit, agent.env.example, and the launchd plist say to restart the agent to reload that file, and that a harness environment variable is a debug override. Precedence is flag (if any), then config root, then env, then the adapter default. `terva-lampi status` prints `harness <id> enabled=<true|false> root=<absolute path or empty> source=<config|env|default>`.

## Acceptance criteria

- [x] Client config accepts optional `harnesses` map per Shape A with the locked precedence and omit/unknown/enable semantics above
- [x] Agent discover/watch/upload honor enable and root; watermarks/CAS unchanged when a harness is disabled
- [x] `status` reports per harness: enabled, resolved root, source (`config` | `env` | `default`)
- [x] Docs/status cover allowlist DX for Cursor global empty-cwd refuse and Cursor CLI missing cwd (no allowlist policy change)
- [x] Deploy examples show Shape A `harnesses` without introducing VPS/restic/frontend scope
- [x] Out of scope remains out: VPS cutover, restic, AgentsView/deja-vu, enrolment API, purge CLI, soft-link

## Definition of done

- [x] All children of this epic are done
- [x] TKT-01M39306SHCKX7FD61WJQB2PDZ Config: harnesses schema
- [x] TKT-01M39306SKH81TGQ69HSV578FF Agent: honor enable + root
- [x] TKT-01M39306SNS6A9VC1RYA8ERTTE status: resolved harnesses
- [x] TKT-01M39306SQYDYS7Y6X1KXRJXM0 Allowlist DX notes
- [x] TKT-01M39306SSJQ9WWGHFWEA840FF Deploy examples for harnesses

## Notes

**agent:cursor/d70d** at 2026-09-24T08:31:21Z

TKT-01M39306SSJQ9WWGHFWEA840FF (Deploy examples for Shape A harnesses) is done. The copy-paste file is deploy/config.json.example. The operator note is the Harnesses section of deploy/README.md. The systemd user unit, agent.env.example, and the launchd plist say to restart the agent to reload config.json, and that a harness environment variable is a debug override under flag (if any), then config root, then env, then the adapter default.

The status line operators can match is `harness <id> enabled=<true|false> root=<absolute path or empty> source=<config|env|default>`.

TKT-01M39306SQYDYS7Y6X1KXRJXM0 (Allowlist DX notes for Cursor cwd refuses) stays ready. This epic stays ready. The deploy-examples acceptance criterion and the matching definition-of-done line are checked. The other lines stay open, including the ones for children that already landed, which those closes left unchecked.

**agent:cursor/d70d** at 2026-09-24T08:35:45Z

This note supersedes the note at 2026-09-24T08:31:21Z. TKT-01M39306SQYDYS7Y6X1KXRJXM0 (Allowlist DX notes for Cursor cwd refuses) landed on main as the squash-merge of PR #45 (c47af03). Every child of this epic is done.

The deploy examples stay in deploy/config.json.example and the Harnesses section of deploy/README.md. The empty-cwd allowlist notes stay in README, docs/policy.md, docs/architecture.md, docs/protocol.md, and the agent, sync, and status help text.

## Summary

Shape A is done. Client config accepts the harnesses map, the agent honors enable and root, and status prints harness <id> enabled=<true|false> root=<absolute path or empty> source=<config|env|default>.

Deploy examples are deploy/config.json.example: loopback server, Claude enabled false, Codex absolute root placeholder. The unit and env notes say to restart to reload, and that a harness environment variable is a debug override.

Allowlist DX for an empty Cursor cwd is the README section and the help text from TKT-01M39306SQYDYS7Y6X1KXRJXM0 (Allowlist DX notes for Cursor cwd refuses). Permit rules are unchanged. VPS cutover, restic, enrolment, purge, and soft-link stayed out.
