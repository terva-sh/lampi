---
schema: 3
id: TKT-01M3MGZ4400WYNA8HBB36TH9KY
title: "Agent self-update: verified upgrade to the lake's release"
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/agent
  - area/ops
assignees: []
milestone: null
parent: TKT-01M3MAV1XM089JPDCG5NH2RAJ6
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/2cf53976
  branch: agent/lake-release
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-2cf53976
  commit: 8578473fafbe70983872c326ffffb938f8e189a4
  session: null
  claimed_at: 2026-09-28T17:30:56Z
  expires_at: null
archive: null
created_at: 2026-09-28T17:28:26Z
updated_at: 2026-09-28T17:30:56Z
created_by:
  id: agent:claude-code/2cf53976
  name: ""
updated_by:
  id: agent:claude-code/2cf53976
  name: ""
extensions: {}
---

## Description

Add `terva-lampi self-update` so an installed agent can upgrade itself. Today the only path is re-running `install.sh`.

### Owner decisions (2026-09-28)

- **Restart:** after replacing the binary, restart the agent's service if a unit exists (systemd user unit `terva-lampi-agent.service`, or launchd `sh.terva.lampi.agent`). `--no-restart` opts out. Reload is not enough: HUP reloads the lakes but keeps running the old binary.
- **Target:** a bare run targets the release the agent's lake runs, capped at the newest GitHub release, so an agent is never ahead of its lake ("Upgrade the lake before any agent", `docs/policy.md`). With no lake release known, it targets GitHub latest. `--version TAG` pins a release and may downgrade. `--latest` ignores the lake.
- **Lake-driven upgrades:** not now. The lake advises: `status` says when the agent is behind the lake's release. Automatic upgrades are filed separately as a draft for later.

### Design, following git-ticket's `self-update`

- **Flags:** `--check` exits 10, 11 or 12 for a patch, minor or major gap. `--dry-run`, `--version TAG`, `--latest`, `--no-restart`, `--lake NAME`.
- **Source:** the GitHub releases API for `terva-sh/lampi`, the same source as `install.sh`. It honours `TERVA_LAMPI_INSTALL_API`, so tests and mirrors reuse the installer's override.
- **Verification:** the archive's sha256 against `checksums.txt` before anything on disk changes. The staged binary must report the target tag from `--version` before it is swapped in, as `install.sh` checks.
- **Replacement:** write beside the target, then rename. The previous binary is kept as `terva-lampi.prev` for a one-step rollback. On Windows, the running binary is renamed aside first.
- **Refusals:** a dev build (`0.0.0`) is refused. A target the process cannot write fails with its path named, and `self-update` never uses sudo.
- **Lake release:** the lake adds `release` to the `POST /v1/hello` answer, from build info. That is additive, and older agents ignore it.

### Alternatives considered

- **Signature verification** (cosign or minisign): no sibling does it, and neither does `install.sh`. It would need a goreleaser `signs:` change across the org. Deferred.
- **`update` as the name:** too easy to confuse with `sync` and `profiles set`, and git-ticket uses `self-update`.
- **Shelling out to `install.sh`:** it needs `curl` and `sh`, which Windows does not have, and it prints rather than restarts.

## Acceptance criteria

- [ ] self-update installs the lake's release, verified by checksum and a version smoke test
- [ ] The agent's service restarts after an update unless --no-restart
- [ ] --check exits 10/11/12 by gap; a dev build is refused
- [x] status says when the agent is behind its lake's release

## Implementation plan

Two PRs stacked on #78, which added releaseVersion().
A: the lake's release in the hello answer (api.Server.Release, set by serve from build info when it parses as a release). A new internal/release package handles parsing, comparison and gap. status prints lake_release and an upgrade line when the agent is behind its lake.
B: terva-lampi self-update, ported from git-ticket's cli/selfupdate.go. Target: the lake's release capped at GitHub latest, or --version, or --latest. It checks the sha256 against checksums.txt, runs the staged binary's --version as a smoke test, keeps the old binary as .prev, swaps by rename, and restarts the systemd or launchd unit unless --no-restart. --check exits 10/11/12.
