---
schema: 3
id: TKT-01M44KG2PM151XTBC5RNFTYWC9
title: Deploy v0.8.0 to the internal lake and the workstation agent
type: task
status: in-progress
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
claim:
  actor: agent:claude-code/9078ac3f
  branch: release/v0.8.0
  worktree: /home/sothr/.cache/agent-scratch/lampi/sdk-port-CTOO/rel
  commit: 34bed399f3c93b5d819ec864b6a21649e02a1057
  session: null
  claimed_at: 2026-10-04T23:20:30Z
  expires_at: null
archive: null
created_at: 2026-10-04T23:20:30Z
updated_at: 2026-10-04T23:30:15Z
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

- [ ] The lake runs v0.8.0 at schema 21 with its counts, lake id, admin group and public URL unchanged, and its MCP endpoint answers 401 without a token
- [ ] The workstation agent runs v0.8.0 with the same lake, profile and allow source, and its next sync re-uploads nothing it had already uploaded
- [ ] The lake checkpoint and the agent backup are recorded, and the rollback for each is written down

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
