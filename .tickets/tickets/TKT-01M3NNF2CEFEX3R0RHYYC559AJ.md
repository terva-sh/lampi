---
schema: 3
id: TKT-01M3NNF2CEFEX3R0RHYYC559AJ
title: "Bays: agent bay requests and terva-lampi bays"
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/7859b064
  branch: bays/agent-nobay
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-7859b064
  commit: 3bfb2c6b705f90327dbe7774924ed7ff6e591ad8
  session: null
  claimed_at: 2026-09-29T16:18:26Z
  expires_at: null
archive: null
created_at: 2026-09-29T04:06:17Z
updated_at: 2026-09-29T16:18:26Z
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

## Implementation plan

Two PRs stacked on the routing child (TKT-01M3NNF29W):

1. bays/agent (6a): `config.BayRequests` on each `lakes` entry (`bays.rules` with the projects matcher read as allow, plus harness, each naming bays; `bays.default`). `upload.Options.Bays`; prepare sets `manifest.bays`. `terva-lampi bays which [PATH] [--harness H]`.
2. bays/agent-nobay (6b): manifests set `bay_aware`. A 409 `no_bay` keeps the session in the outbox and lets the run go on; `Result.NoBay` goes to last_attempt.json, `status` (`no_bay:`), `sync` and agent stderr (once per session), and the agent report (`last_sync.no_bay`). `terva-lampi bays` and `status` list the bays hello says this device may write, with `bays_refused` for asked-for names not among them.

## Notes

**agent:claude-code/7859b064** at 2026-09-29T16:18:26Z

Decisions, with the alternatives that lost:

- **Union of every matching rule, else `default`.** First-match-wins lost: a session in a client folder that is also a codex session should be able to ask for both. `default` applies only when no rule matched, so a rule can steer a session away from the personal default.
- **Rules read as allow (exact), not deny.** This matches the lake's own rules (TKT-01M3NNF29W). A doubt must not ask for a bay. A test pins that `/src/client` does not catch `/src/client-y`.
- **`bays` only on entries of the `lakes` map.** The legacy top-level lake has none; the docs tell the user to move it into the map. Adding a top-level `bays` would give one lake two places to be described, the problem the lakes map already refuses for server and token_file.
- **Lake profile suggestion deferred.** The agent decodes a profile strictly (`config.decodeStrict`), so an older agent would refuse a profile that carries `bays` and stop applying its profile at all. Doing it needs either a lake that sends `bays` only to agents that announce support, or a separate document. Neither is in this ticket; the scope line "A lake profile may suggest both" is not done.
- **`bay_aware` lands in 6b, not 6a.** 6a alone would make a lake with the default off answer this agent with 403, which reads as a token refusal. Keep 6a and 6b close together.
- **No-bay sessions stay in the outbox and are re-posted every pass.** A separate parking state lost: the blobs are already stored, so a repost is one manifest, and it uploads the moment an admin fixes grants, rules or the default, with nobody touching the machine.
- **The ACK's `refused_bays` is not surfaced per session.** `status` compares config against hello's writable list instead, which is where a user can act on it.
