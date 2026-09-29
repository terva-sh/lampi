---
schema: 3
id: TKT-01M3NMHDWRAC7XDV1VF2XESB8V
title: "lakes adopt: pin a lake a machine already syncs to, so it takes profiles"
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/agent
  - area/auth
  - area/docs
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/16ebd168
  branch: agent/lakes-adopt
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-16ebd168
  commit: a43c5ce9142557e3a943e0aa4a8455954cb3d256
  session: null
  claimed_at: 2026-09-29T03:50:13Z
  expires_at: null
archive: null
created_at: 2026-09-29T03:50:06Z
updated_at: 2026-09-29T03:50:13Z
created_by:
  id: agent:claude-code/16ebd168
  name: ""
updated_by:
  id: agent:claude-code/16ebd168
  name: ""
extensions: {}
---

## Description

A machine that syncs to a lake with a device token but never registered has no pinned lake key, so its agent never fetches the lake's profile. The workstation's own agent is one: it syncs to the loopback lake as `token-1`. On 2026-09-29 the owner tried the v0.3.0 Review page's Allow selected on its 19 projects and got only "No rule can be added". The owner asked for the agent to be able to adopt the lake and take its profiles, tested on that original importer, with documentation for people and for model agents.

### What the code already had

- The lake serves signed profiles to token-file devices: auth resolves the token to a device, and `ResolveProfile` serves its profile or the default.
- The client verifies a profile only under a pin (`lakeprofile.Pinned`). A legacy top-level lake has none, and `register` refuses to replace it in place.
- The legacy default lake, and a `lakes.default` entry, share the machine id (`machine.json`), the token path and the state directory `lakes/default/`. Moving between them with the name kept changes none of those.

### Design

- `terva-lampi lakes adopt [NAME]` pins the lake the machine's token already reaches:
  - the key list over a fresh nonce, signed by an active key;
  - a pinned `hello` with the machine's token;
  - fingerprint confirmation, as `register` does it;
  - the profile verified under the pin, whose payload gives the device id.
- The legacy top-level `server`, `token_file` and `projects.allow` move into `lakes.default`. Top-level `projects.deny` and unknown keys stay.
- `--allow-from profile` drops the local allow rules. Before any write, adopt reads every session and lists each project the change would stop uploading, and refuses unless `--force`.
- The dashboard marks a token-file device whose report names no profile as "fetches no profile", on the devices page, the device page and the Review queue, and says to run `lakes adopt`. The lake infers this, so agents already in the field are covered without a protocol change.

### Alternatives rejected

- **Automatic pinning** by the agent at start, or of a co-located lake. It is trust on first use with no person checking. `docs/policy.md` treats the fingerprint check as the only defense against a forged lake, and on loopback anything that binds `127.0.0.1:8787` first would be pinned.
- **Revoke the token device and register again.** It works, but makes a new device, needs root on the lake for the code and the revoke, and without `--lake default` it posts every session again under a new machine id.
- **Reporting the local rules in the inventory**, so the dashboard offers to copy them into a profile. Useful, but a protocol addition. Left for a follow-up, since adopt already lists what a profile misses.

## Acceptance criteria

- [x] lakes adopt pins the legacy default lake in place: same machine id, token, device and watermarks, next sync uploads nothing
- [x] adopt refuses without a confirmed fingerprint, with a token the lake does not know, and before writing when a project would stop uploading
- [x] The dashboard says a token-file device with no profile fetches none and to run lakes adopt
- [x] docs for people (registration-and-lakes, cli, web-dashboard, web-api) and for model agents (AGENTS.md)
- [ ] The workstation's original importer (token-1) is adopted and takes the default profile
