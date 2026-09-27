---
schema: 3
id: TKT-01M3FHHBKTMJCHCMQQQJZKQTMF
title: "Lake base config: signed agent profile, fetch, cache and merge"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/agent
  - area/protocol
assignees: []
milestone: null
parent: TKT-01M3FHHBCJ12FXKNTB6Z138F6N
origin: null
dependencies:
  - TKT-01M3FHHBFAYKEK0NAXVR91969G
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-26T19:02:11Z
updated_at: 2026-09-27T01:40:48Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Part of the agent onboarding epic. Let a lake publish a standard base configuration that its agents fetch at registration and keep current.

- The lake operator writes a profile file (the default profile, plus named profiles a code can select). Allowed fields: `harnesses`, `agent.debounce` and `agent.debounce_max`, `redaction`, `projects.deny`, and `projects.allow`. Any other field fails the load, the way an unknown harness key does today.
- Each device records its profile, set from the code at registration. `serve devices set-profile NAME PROFILE` changes it, and the device picks up the change at its next fetch.
- `GET /v1/agent/config` returns the profile for the calling device, signed with the lake key, with a version. The registration response carries the same document.
- The agent caches the last verified copy per lake. It refetches on start and on a slow interval. A copy that does not verify against the pinned key is refused and the cached copy stays in use.
- Merge rules: local `config.json` overrides every field. A local deny wins over a lake allow. A lake's `projects` rules apply only to uploads to that lake. `terva-lampi agent config` prints each effective value and whether it came from the local file or from which lake.

## Acceptance criteria

- [x] The lake serves a signed profile at GET /v1/agent/config, and a profile with a field outside the allowed set fails the load
- [x] The agent verifies the profile against the pinned key and keeps its last good copy when a fetch fails or the signature is bad
- [x] A local deny wins over a lake allow, and one lake's rules never apply to uploads to another lake
- [x] agent config names the source of each effective value

## Implementation plan

Server: config.Profile (harnesses, agent debounce, redaction, projects) decoded strictly; Validate refuses a harness root and redaction.upload_hits. profiles.json ({profiles:{name:{...}}}) in the lake directory or --profiles; missing file = empty default. Loaded at start (bad file stops serve), reloaded on SIGHUP (bad file keeps the old set). GET /v1/agent/config (authed) signs AgentConfigPayload{lake_id, device_id, profile, version=sha256 of profile, issued_at, config} with context agent-config/v1; device profile from the existing devices.profile column; unknown profile 404. serve devices set-profile NAME PROFILE checks the profile exists, writes the column, audits device.profile. Client: lakeprofile.Verify needs a pin (lake_id, key_id, public_key), checks signature, lake_id and strict profile; Load/Save cache lakes/<name>/profile.json. loadClientConfig applies cached profiles: ApplyMachineProfiles (local wins, then first lake in order), ApplyLakeProfile (profile allow only when local lake has none; profile deny appended). Used by agent, reload, sync, status, discover, agent config. Agent: watchProfile per pinned lake fetches at start and hourly; new version -> save and lakeSet.reload (serialized by reloadMu); machine-wide change is logged as needing a restart.

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-26T21:37:47Z

Where this differs from the ticket text, and why:

- A profile cannot set a harness `root` or `redaction.upload_hits`, although the ticket listed `harnesses` and `redaction` as allowed.
  - A root would let the lake choose which directory the agent reads.
  - `upload_hits` would upload files the ruleset flagged as secrets.
  - Either one lets a compromised or forged lake widen what leaves the machine. Harness `enabled` stays, since harnesses are on by default and a profile can only turn one off.
  - The refusal happens twice: on the lake when the file loads, and in the agent when it decodes the profile, because the agent does not trust the lake.
- A lake's `projects.allow` applies only when `config.json` gives that lake no allow rules of its own.
  - Rejected: taking the union of the local and lake allow lists. A machine owner who wrote a narrow allowlist would find it widened by the lake.
  - This is one reading of "local config.json overrides every field".
- Machine-wide fields (harnesses, debounce) go to the first lake in order that sets them: default first, then by name.
  - Rejected: taking the union of every lake's harness disables, because one lake could stop uploads to all the others.
  - Rejected: per-lake harness sets, because harnesses are watched once for all lakes.
- A lake with no pin gets no profile, and `sync` uses only the cached copy.
  - A profile is accepted only when it chains to a key pinned at registration.
  - Fetching one without a pin would take configuration from whatever answers at that URL.
- The version is a content hash of the profile, not a counter, so the lake keeps no state for it.
  - Rejected: a monotonic counter to catch replay. An attacker able to replay an older signed profile is already inside TLS to the lake. The signed `issued_at` is available if rollback checks are wanted later.
- A changed profile goes through the same reload as SIGHUP. Reloads are now serialized by `reloadMu`, because each lake's fetch loop can ask for one.

Evidence:
- TestLoadProfiles
- TestAgentConfigIsTheDevicesProfileSigned
- TestServeDevicesSetProfile
- TestServeRefusesAProfilesFileWithAFieldOutsideTheAllowedSet
- lakeprofile TestVerify/TestSaveLoad (other key, other lake id, root, upload_hits, unknown field, no pin, hello-context signature, tampered payload, edited cache)
- TestApplyMachineProfilesLocalWinsThenFirstLake
- TestApplyLakeProfileNeverWidensALocalAllowAndDenyWins
- TestOneLakesProfileNeverAppliesToAnotherLake
- TestAgentFetchesThePinnedProfileAndUploadsWhatItAllows
- TestAgentRefusesAProfileSignedByAnotherKeyAndKeepsItsCache
- Full -race suite, golive drills, and GOOS=windows vet are green.

Registration (TKT-01M3FHHBJ) should return the same signed document so a new agent has a profile before its first fetch. register (TKT-01M3FHHBR) should write it with lakeprofile.Save.

**agent:claude-code/e4a47e8c** at 2026-09-27T00:04:01Z

Review 931 on PR #16. (1) accepted, a8931a8: a pinned lake does not push until the first profile fetch has answered, so the profile's deny rules apply to the first upload. Cost: a pinned lake that is down waits out the fetch timeout (up to a minute) before its first push, and a runner stopped before that leaves its outbox on disk for the next start. (2) rejected: the only redaction field is upload_hits, which a profile may not set, and strict decoding refuses any other key, so there was nothing being dropped; a TestLoadProfiles case pins that. Loosening what profiles may carry stays an owner decision. (3) accepted, a8931a8: the cache is keyed on profile name as well as version; a rename with the same content is saved without a reload.

**agent:claude-code/e4a47e8c** at 2026-09-27T01:01:02Z

Review 973 on PR #16, accepted in 1ecc3eb. A verified first profile with new rules that cannot be saved keeps the runner held, because the reload that applies the rules reads the saved copy. A later fetch that saves it releases the runner. A failed or unverified fetch still releases it on the cached copy. Cost: with a state directory that cannot be written, uploads wait for the hourly retry, which fails closed. Also on this layer: the fan-out fix's startAgentLakes had read config.json without the lake profile and built lakes without their config, so no lake was pinned. That surfaced only after the merge, as profile tests timing out. Start now goes through the same builder as reload.

**agent:claude-code/e4a47e8c** at 2026-09-27T01:40:48Z

Review 984 on PR #16, both accepted in aae2992. reload now returns whether the config it read is in force. A saved new profile stays pending and its reload is retried on every tick, and the first upload waits until one succeeds, so a profile deny can no longer be bypassed by a failed reload. Once the runner is released it is not held again; a later failed reload is only retried. The machine-wide settings a reload compares against are now the ones the agent started with, not the last config read.

## Summary

Lakes publish strict, signed profiles at GET /v1/agent/config, chosen per device with serve devices set-profile and reloaded on SIGHUP. Agents with a pinned lake fetch at start and hourly, verify against the pin, cache per lake, and merge with config.json winning; agent config prints each value's source. Profiles cannot set harness roots or upload_hits.
