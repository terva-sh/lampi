---
schema: 3
id: TKT-01M3NNF29WCV5F1D8QSBWPM6M0
title: "Bays: manifest bays field and lake routing rules"
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/7859b064
  branch: bays/routing-cli
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-7859b064
  commit: 6a7edb895bcd6b8ce3ea59d3ec3692b7099107fb
  session: null
  claimed_at: 2026-09-29T15:58:53Z
  expires_at: null
archive: null
created_at: 2026-09-29T04:06:17Z
updated_at: 2026-09-29T15:59:04Z
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

## Implementation plan

Two PRs, stacked on the read-scope child (TKT-01M3NNF27A):

1. bays/routing: the routing engine and the manifest wiring.
   - protocol: Manifest gains `bays` (at most 16 refs, each at most 128 bytes) and `bay_aware`; ManifestAck gains `refused_bays`; ErrorBody gains `code`, with `no_bay` the only one.
   - catalog schema 20 (`migrateBayRules`): `bay_rules` (match JSON with the profile rule fields, harness, action hold|add|deny, bay) and `session_holds` (held | flagged | released).
   - `routeSession` runs inside the ingest transaction on every manifest:
     - resolve the requests against the device's write grants and record each with its outcome;
     - a live hold, or a hold rule not yet applied to this session and bay, wins: a new session goes to the hold bay alone, a stored one is flagged, and its requests are recorded as held;
     - otherwise add (accepted ∪ add rules) − deny rules;
     - a new session with no bay goes to the default, or is refused with ErrNoBayForSession when the default is off.
   - `ReleaseHold` resolves held requests again, places them, and takes a held session out of the hold bay (default if left in none).
   - api: the manifest handler passes the device as `catalog.Route`; ErrNoBayForSession returns 409 `no_bay` when `bay_aware` is set and 403 otherwise.
2. bays/routing-cli: hello lists feature `bays` and the device's writable bay names; `serve bays rules|rule|unrule|holds|release`.

## Notes

**agent:claude-code/7859b064** at 2026-09-29T15:58:53Z

Decisions made while building routing, with the alternatives that lost:

- **The engine and the API wiring ship in one PR.** The catalog alone treats an empty Route as "every bay accepted". If the engine merged before the handler passed the device, any device could ask its way into any bay.
- **Rules match like an allow rule (exact) and can name a harness alone.** The deny-rule reading, where a doubt counts as a match, lost: a bay rule places access rather than refusing upload, so a doubt must not add a session to a bay. TKT-01M3NQ83 (Allow on a folder project allows everything under it) is still a draft. So `cwd_prefix` here also covers subfolders, the docs say to use `cwd_hash` for one folder, and a test pins that a sibling folder `/src/ab` is not caught by `/src/a`.
- **Hold wins over requests and adds, including for a flagged stored session.** Its later requests are recorded as held rather than placed. The alternative was to flag but keep placing. It lost because it would widen access to a session an admin was asked to review.
- **A hold released once does not return for the same bay.** Re-holding on the next manifest would make release useless.
- **Deny does not keep a session out of the default when nothing else places it.** The rule that a session always lands somewhere, or is refused only when the default is off, comes first.
- **The ACK lists refused refs without reasons.** Reasons (no such bay / not granted) stay in `session_bay_requests`. Sending them would tell a device that a bay it may not write exists.
- **An old agent gets 403 for no_bay.** It already treats that as a token refusal and waits the full 5-minute backoff. 409 would send it into its missing-blob handling.
- **ReleaseHold records its membership changes as via=cli.** The dashboard (TKT-01M3NNF2K3) may need a via parameter when it adds release.

**agent:claude-code/7859b064** at 2026-09-29T15:59:04Z

Corrects the previous note's 403 bullet: an old agent does not treat a manifest 409 as missing blobs (only 'prefix mismatch' has special handling, internal/upload/upload.go). It retries a 409 on the ordinary backoff from 2s, doubling. A 403 counts as upload.Unauthorized and waits the full 5 minutes, which is why old agents get 403.
