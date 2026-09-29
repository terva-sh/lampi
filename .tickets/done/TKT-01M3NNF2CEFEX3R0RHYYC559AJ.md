---
schema: 3
id: TKT-01M3NNF2CEFEX3R0RHYYC559AJ
title: "Bays: agent bay requests and terva-lampi bays"
type: task
status: done
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
updated_at: 2026-09-29T19:16:17Z
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

- [x] Bay request rules and default_bays route one session to two bays
- [x] terva-lampi bays which PATH names the lake, bays and deciding rule
- [x] A no-bay refusal keeps the session pending and shows in status

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

**agent:claude-code/7859b064** at 2026-09-29T19:16:17Z

Review rounds, PRs #160 and #161:

- Review 1440 (#160): bays which printed only the refusal for a project the lake refuses. It now also names the bays the session would ask for, and the rules that named them, so a rule can be checked before the project is allowed.
- Review 1442 (#161): no-bay notices were deduplicated only on a pass that finished, so every failing pass repeated them. A failing pass now uses changeLog.add: it prints only new lines and forgets none. Using fresh there was rejected, because a pass that failed partway would forget sessions it never reached.
- Review 1449: a failed pass wrote no_bay 0 to last_attempt.json. It now keeps the previous attempt's waiting sessions until a pass finishes. Deriving the list from the outbox was rejected, because the outbox does not record why an entry waits.
- Review 1451: rejected. The claim was that manifests queued by an older agent go out without bay_aware. They are never posted as stored: outbox.Pending has no production caller, and a pending session is prepared again every pass, with BayAware set.
- Review 1454: with five named, a sixth waiting session was not counted. The count is now the union when the previous attempt named every session it counted, and otherwise a lower bound.

## Summary

Landed in #160 (51ad973) and #161 (5583dc0). In config.json, a lakes entry takes bays.rules and bays.default, and a session asks for the union of every matching rule's bays, or the default. Every manifest carries bays and bay_aware. terva-lampi bays which PATH names, per lake, whether the session uploads, the bays it asks for or would ask for, and the deciding rule. A 409 no_bay keeps the session in the outbox, and the rest of the run goes on. It is listed under no_bay in status and last_attempt.json and printed once per session, and last_sync.no_bay is in the agent report. terva-lampi bays and status list the bays hello says the device may write, plus bays_refused. Tests: internal/cli/bayscmd_test.go, internal/upload/attempt_test.go, internal/cli/agent_test.go.
