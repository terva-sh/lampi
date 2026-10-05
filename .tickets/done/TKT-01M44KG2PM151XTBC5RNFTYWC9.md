---
schema: 3
id: TKT-01M44KG2PM151XTBC5RNFTYWC9
title: Deploy v0.8.0 to the internal lake and the workstation agent
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
assignees: []
milestone: null
parent: null
origin: null
dependencies:
  - TKT-01M44K9704JXEJ2Q4PA3HA2SB6
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-04T23:20:30Z
updated_at: 2026-10-05T00:00:09Z
created_by:
  id: agent:claude-code/9078ac3f
  name: ""
updated_by:
  id: agent:claude-code/9078ac3f
  name: ""
extensions: {}
---

## Description

The owner asked on 2026-10-04 to deploy v0.8.0 (TKT-01M44K9704JXEJ2Q4PA3HA2SB6, Release v0.8.0) to their workstation's lake and capture agent. That request is this ticket's promotion. The lake runs v0.7.0 and the agent v0.6.0, at catalog schema 21. v0.8.0 adds no migration and changes no config.

### Two steps

1. **The lake.** The owner runs the operator script from a deploy bundle in the agent-handoffs directory, outside the repository, as root. The script is the v0.7.0 bundle's with the versions changed and one check added: after the install, an anonymous POST to `/api/read/v1/mcp` must get 401, where v0.7.0 answers 404.
   - It checks every precondition before anything stops.
   - It pauses the agent, then stops and backs up the lake with the installed binary. It re-hashes and counts the copy.
   - It installs v0.8.0, then checks health, 401s, the admin group, schema, counts, the lake id and the public URL.
   - It resumes the agent and waits for a forced sync.
2. **The agent.** Once the lake reports v0.8.0, `terva-lampi self-update` installs the release the lake runs. It checks the download against `checksums.txt`, keeps `terva-lampi.prev`, and restarts the user unit. The owner asked for this step, so an agent session may run it. It backs up `config.json`, the token and the state directory first, as AGENTS.md asks.

### What to expect on this workstation

The upgraded agent starts reading Cursor CLI ACP sessions under `acp-sessions/`. On 2026-10-04 there were 9 stores:
- Three are over the 256 MiB cap. Each prints a `skipped` line.
- Six are under it, the largest 218 MiB. Each is exported only if the profile allows its cwd, and only once it has gone 5 minutes unwritten.
- An export is built in memory, so a large one can take a few GB while it is built.

### Rollback

A binary swap for each:
- **The lake:** the checkpoint keeps the old binary.
- **The agent:** `terva-lampi.prev`.

## Acceptance criteria

- [x] The lake runs v0.8.0 at schema 21 with its counts, lake id, admin group and public URL unchanged, and its MCP endpoint answers 401 without a token
- [x] The workstation agent runs v0.8.0 with the same lake, profile and allow source, and its next sync re-uploads nothing it had already uploaded
- [x] The lake checkpoint and the agent backup are recorded, and the rollback for each is written down

## Implementation plan

1. **Bundle.** `deploy-v0.8.0-UAFPZgSH` in the lampi agent-handoffs directory holds:
   - the GitHub v0.8.0 linux_amd64 archive and binary, checked against the release's `checksums.txt`, which is kept as `release-checksums.txt`;
   - `operator-deploy.sh` and `README.md`;
   - the unit and drop-in it compares, byte-identical to the v0.7.0 bundle's and to the installed files;
   - `REVISION` and `SHA256SUMS`.
2. **The lake.** The owner runs `operator-deploy.sh` as root and returns its output. A failure before the install restores the lake and the agent. A failure after the install keeps capture paused and the checkpoint.
3. **The agent.** Back up `config.json`, `machine.json`, the token, the state directory and the v0.6.0 binary into a scratch directory. Then run `terva-lampi status`, which should say the agent is behind a v0.8.0 lake, and `terva-lampi self-update`. Check `--version`, and check that `agent config` shows the same lake id, profile and allow source.
4. **Verify.** Watch the first pass. Previously uploaded sessions should read unchanged. Large Cursor CLI ACP stores print `skipped` lines.
5. **Record.** Note the checkpoint, the agent backup, and the counts before and after. Close this ticket through a tickets PR.

## Notes

**agent:claude-code/9078ac3f** at 2026-10-05T00:00:09Z

### Deployed, 2026-10-04

**The lake.** The owner ran the bundle's `operator-deploy.sh` as root, and the lake started on v0.8.0 at 18:52:20 local time. The script restarts the agent only as its last step, after every post-install check has passed:
- health and the 401s;
- the MCP 401;
- the admin group;
- `migrate --check` at 21;
- integrity and counts;
- the lake id;
- the public URL.

The agent unit restarted at 18:52:22 and synced at 18:58 (refused 4, quarantined 17, as before), so the script reached that step. It kept its checkpoint, `terva-lampi-pre-v0.8.0-os9QUzTt`, beside the earlier ones.

Checked afterwards as the owner's user:
- `/usr/local/bin/terva-lampi --version` prints `terva-lampi v0.8.0 (f150332a55c8)`, and the unit is active.
- `/healthz` answers 200 on the loopback and on the public URL.
- `/v1/stats` and `/api/web/v1/overview` answer 401.
- An anonymous POST to `/api/read/v1/mcp` answers 401, where v0.7.0 answered 404.
- The signed keys document names the lake id the agent pinned.
- `status` reports `lake_release: v0.8.0`, with normalization ready=774 and nothing pending or failed.

The serve journal and the checkpoint are not readable as that user, so the counts are known from the script's check rather than read here.

**The agent.** Steps, in order:
1. The unit was stopped.
2. The config directory (`config.json`, `machine.json`, the token), the state directory and the v0.6.0 binary were copied to a scratch directory, and `diff -r` matched the copies.
3. `terva-lampi self-update --no-restart` installed v0.8.0. It is byte-identical to the GitHub linux_amd64 binary checked for the release, and v0.6.0 is kept as `terva-lampi.prev`.
4. Before the start, `agent config` showed the same lake id, profile version, 151 allow rules from `lake:default`, and machine id.
5. The unit was started at 18:58:57 on the new binary.

The first passes read `unchanged 255`–`256` and `quarantined 17`, as before. The 1–2 uploads were transcripts being written at the time. `refused` went from 4 to 12. The 8 new refusals are Cursor CLI ACP sessions, now read under `acp-sessions/`, whose cwds are outside the profile's allow rules, so nothing was exported or uploaded. No `skipped`, error or panic lines appeared. The agent's RSS was 240–290 MiB.

**Rollback.**
- **The lake:** stop it, restore the checkpoint's `terva-lampi.before` to `/usr/local/bin/terva-lampi`, and start it.
- **The agent:** move `~/.local/bin/terva-lampi.prev` over `terva-lampi` and restart the user unit.

The scratch copy of the agent's config and state is swept once idle for a week. `terva-lampi.prev` and the lake checkpoint stay.

## Summary

v0.8.0 runs on the workstation's lake and capture agent.

- **The lake.** The owner ran the bundle's operator script, which checkpointed the lake, installed v0.8.0, and verified the schema (21), counts, lake id, admin group and public URL. The new MCP endpoint answers 401 without a token.
- **The agent.** It went from v0.6.0 to v0.8.0 through `self-update`, after a backup of its config, token and state. It keeps the same lake, profile and allow rules. Its passes re-upload nothing.
- **Cursor CLI ACP sessions.** They are now read. All 8 found are outside the allow rules, so none was exported.

Rollback is a binary swap for each: the lake checkpoint keeps the old lake binary, and `terva-lampi.prev` keeps the old agent.
