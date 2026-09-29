---
schema: 3
id: TKT-01M3NMHDWRAC7XDV1VF2XESB8V
title: "lakes adopt: pin a lake a machine already syncs to, so it takes profiles"
type: task
status: done
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
claim: null
archive: null
created_at: 2026-09-29T03:50:06Z
updated_at: 2026-09-29T05:06:37Z
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
- [x] The workstation's original importer (token-1) is adopted and takes the default profile

## Notes

**agent:claude-code/16ebd168** at 2026-09-29T03:51:18Z

Rehearsed 2026-09-29 against a scratch v0.3.0 lake with a token-file device (ws), with a v0.2.0 agent running on the legacy config, which is the workstation's situation. First, lakes adopt --fingerprint (branch build) pinned it: the running agent logged 'reload: restarted default', then synced with unchanged 5 and uploaded 0. The config moved into lakes.default with the local allow rule and device_id dev_vdveh…. Second, --allow-from profile against a default profile that allowed one checkout refused and listed the lampi remote (4 sessions, +3 checkouts). Third, with a git_remote rule added to the profile, the switch succeeded: agent config showed allow_source=lake:default, the agent reloaded and applied the profile, the next sync uploaded 0, and serve devices list shows the same ws device, now bound to the machine. just ci (GOFLAGS=-mod=mod, XDG isolated) passed.

**agent:claude-code/16ebd168** at 2026-09-29T04:07:45Z

### Live run on the workstation's importer, 2026-09-29

Branch build a97b591; the running agent is still v0.2.0. Before anything changed, the config, token, machine.json, state and binary were copied to `~/.local/state/agent-handoffs/lampi/agent-pre-adopt-TSws47aF/`.

1. **Pin.** `lakes adopt --fingerprint SHA256:X3+sVB7…` pinned lake_u3cpc5lo4dwujlk5il3mpjepai (key 5f7fac541ec9bf33), device dev_3pturjpy67cdkl4dxblfr2tp7u, profile default sha256:e057488d835e22bf. The owner took the fingerprint from `serve identity` on the lake host. The agent logged `reload: restarted default`, and its sync showed unchanged 100 with 3 uploaded; those 3 were live, growing sessions, not a re-send. machine.json is unchanged.
2. **Switch.** `lakes adopt --allow-from profile` found nothing that would stop uploading, so it switched: 28 local rules removed, and the profile allows 97. agent config shows allow_source=lake:default.

### Problem found: the switch widened what uploads

Refused sessions went from 83 in 20 projects to 23 in 6. Measured with `agent refused` under the backed-up config against the current one, **60 sessions in 14 projects started uploading**, and none stopped. They include `/home/sothr`, `/home/sothr/workspace`, `/home/sothr/agent_home`, Sothr-Ledgers/brokkr.git (30 sessions), agent-session, tuohi, Sothr-Infrastructure repos, and `/home/sothr/fleet/hub`. The cause is that the default profile holds `cwd_prefix /home/sothr`, plus `/Users/sothr` and `/Users/drewshort`. Folder Allows made for other devices' home-directory sessions cover every path below them. Adopt guards narrowing only; it has no guard against widening.

### Also open

The sixth terva-review, on a97b591, has two medium findings:
- the harness-off list filters by this lake's rules only, not every lake's;
- `no_profile` cannot tell an unpinned device from a pinned one whose first fetch is pending.

Neither is fixed yet.

**agent:claude-code/16ebd168** at 2026-09-29T04:37:26Z

Owner decisions, 2026-09-29, on the widening in the previous note:
- keep the 60 sessions uploaded;
- narrow default's home-folder rules;
- the workstation stays on default so it follows fleet updates.

Prepared in the external handoff `narrow-default-A6uVhshY`, for the owner to run as root:
- The three cwd_prefix rules /home/sothr, /Users/sothr and /Users/drewshort become cwd_hash rules, which match those exact folders.
- 6 git_remote and 6 cwd_hash rules are added for the projects the workstation uploads today only through /home/sothr.
- On the workstation, agent refused gives the same result under both profiles: 23 sessions in 6 projects.
- The Macs were not checked. A project there that only /Users/* allowed goes back to the Review queue.
- apply.sh refuses if default moved past sha256:e057488d835e22bf.

**agent:claude-code/16ebd168** at 2026-09-29T04:39:08Z

The owner applied the narrowed default at 04:38Z: revision 9, sha256:5c6a11a9c51918f4, replacing revision 8 (e057488d). The workstation agent applied it within about 16 s: projects_allow=109, allow_source=lake:default. agent refused is unchanged at 23 sessions in 6 projects.

## Summary

Landed in PR #133 (merge 1efaf2d on main, synced to GitHub). `terva-lampi lakes adopt [NAME]` pins a lake a machine already syncs to, in place, keeping its machine id, token, device and watermarks. It checks the lake's key list against a fingerprint from the lake host, then the token, then fetches the profile under the pin. `--allow-from profile` hands the allow rules to the profile. It refuses when a project or harness would stop uploading (unless `--force`), and asks before uploading more on a terminal or refuses without one (unless `--yes`). Its writes are held under the config lock against a snapshot of config.json and the cached profiles.

The dashboard marks devices that fetch no profile, using the agent report's new `pinned` field and inferring it for older agents, and offers them no Allow. Docs: registration-and-lakes, cli, web-dashboard, web-api, protocol, and an AGENTS.md section for model agents.

Live: on 2026-09-29 the workstation's importer token-1 was adopted as dev_3pturjpy67cdkl4dxblfr2tp7u with nothing re-sent. It then switched to the default profile, which uploaded 60 newly allowed sessions that the owner chose to keep. default was narrowed to revision 9 (home-folder cwd_prefix rules replaced by exact cwd_hash rules), and the agent took it about 16 s later.

Follow-ups filed as drafts: TKT-01M3NQ8324 (Allow on a folder project allows everything under it) and TKT-01M3NQ2RT5 (normalize fails a whole session on a torn line).
