---
schema: 3
id: TKT-01M44K9704JXEJ2Q4PA3HA2SB6
title: "Release v0.8.0: MCP recall tools and Cursor CLI ACP sessions"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/ci
  - area/ops
  - area/docs
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/9078ac3f
  branch: release/v0.8.0
  worktree: /home/sothr/.cache/agent-scratch/lampi/sdk-port-CTOO/rel
  commit: 99c5e3356e82269f24cb7d039dfdd7f0545171dd
  session: null
  claimed_at: 2026-10-04T23:16:45Z
  expires_at: null
archive: null
created_at: 2026-10-04T23:16:45Z
updated_at: 2026-10-04T23:18:47Z
created_by:
  id: agent:claude-code/9078ac3f
  name: ""
updated_by:
  id: agent:claude-code/9078ac3f
  name: ""
extensions: {}
---

## Description

On 2026-10-04 the owner asked to finish the MCP follow-ups (TKT-01M445H1DMEXTD11PXQVSFMQQ3) and cut a release for their workstation's lake and agent. That request is this ticket's promotion.

The release is v0.8.0. The repository bumps the minor version for a new feature (v0.6.0 Grok Build, v0.7.0 Grok Bot), and this release adds two: the MCP recall tools, and Cursor CLI ACP sessions. The alternative, v0.7.1, was rejected because a patch release here has carried fixes only.

### What it carries

Since v0.7.0 (478435f), main gained the following.

**MCP recall tools**
- #186: the lake serves `/api/read/v1/mcp`. Its tools are `search`, `read_events` and `copy_excerpt`. The caller is a read token that holds `events:read`, and it reads within the token's scope. Each `tools/call` is audited.
- #187: `terva-lampi mcp` serves those tools to an agent over stdio.
- #195: the endpoint runs on `modelcontextprotocol/go-sdk` v1.8.0.
- #199: bay-scoped tokens are tested through the endpoint. `SessionBayNames` now honours a narrowing to named sessions.
- #200: tool calls are limited per read token, to a burst of 30 and then 2 a second.
- #201: the bridge keeps at most 8 requests open to the lake. It honours `notifications/cancelled`, and it logs a notification the lake refused.

**Cursor CLI**
- #189: the export drops `blobEncryptionKey`.
- #190: an unchanged session is skipped without a new export. A session whose every artifact is quarantined is skipped without being read; this applies to every harness.
- #191: ACP sessions under `acp-sessions/` are read. A session written to in the last 5 minutes is held. A store over 256 MiB is skipped, with a line that names it.
- #192: the projector finds each row's offset in one pass.
- #196: the export is cut between blob rows, so an export over 32 MiB re-sends only the chunks that changed. No protocol change.

**Dependencies**
- #193: `x/crypto` v0.45.0 → v0.57.0, `x/sys` v0.47.0 → v0.48.0.
- #194: `klauspost/compress` v1.17.9 → v1.20.1.
- #195: `go-sdk` v1.8.0, which brings `jsonschema-go`, `segmentio/encoding`, `uritemplate` and `x/sync`.
- #200: `x/time` moves to the direct block.

### Upgrading

- **No migration.** `git diff v0.7.0 -- internal/catalog/catalog.go` is empty, and the catalog stays at schema 21.
- **Normalizers.** Of the normalizers, only the Cursor CLI projector changes:
  - #189 drops `blobEncryptionKey` from what it projects.
  - #192 takes each row's `content_ref` offset from one decoder pass, where the old substring search could match another row.

  Sessions already normalized are not queued again. A Cursor CLI session keeps its derived events until its next upload, or until `serve normalize` queues it.
- **Upgrade order.** Upgrade the lake first, then the agents, as `self-update` already requires.
  - `terva-lampi mcp` needs a v0.8.0 lake. An older lake answers 404 on the endpoint, and the bridge reports that as a lake older than its MCP endpoint.
  - A v0.8.0 agent works against a v0.7.0 lake: #196 changes no protocol.
- **Rollback** is a binary swap. A lake rolled back drops the MCP endpoint and keeps its read tokens and their audit events.

## Acceptance criteria

- [x] A lake at v0.7.0 starts on a build of the final main with nothing to migrate, keeps its counts, and passes health and fsck; a v0.6.0 agent and the new agent each sync to it and upload nothing new
- [ ] The release is tagged on both forges and its archives and image name the tag
- [ ] self-update from the published v0.6.0 agent installs the published v0.8.0 against a v0.8.0 lake
- [ ] Release notes say there is no migration, rollback is a binary swap, which normalizer changed, the upgrade order, and describe the MCP endpoint and bridge

## Implementation plan

1. **Rehearse** on the final main, on a scratch lake at 127.0.0.1:18998 with its own HOME and XDG, apart from the live lake and agent.
   - Seed: the published v0.7.0 binary creates the lake, and the published v0.6.0 agent uploads a Claude session. Both binaries are checked against their release's `checksums.txt`. These are the versions the workstation runs.
   - The new agent syncs against the v0.7.0 lake.
   - Then stop the lake, back it up with v0.7.0, and run `serve migrate --check` with the new build. Start the new build on the same data.
   - Check health, `/v1/stats`, `serve normalize --status` and `serve fsck`.
   - Check that the v0.6.0 agent and the new agent each upload nothing new.
2. **Gate.** Run `GOFLAGS=-mod=mod just ci` and `just release-check` on the release tree.
3. **Tag.** Tag v0.8.0 (annotated) on main, which must be the same commit on both forges. Push the tag to origin and github.
4. **Verify.** Check the archives against `checksums.txt` on both forges, the binaries' `--version`, and `ghcr.io/terva-sh/lampi:0.8.0` and `:latest`.
5. **Rehearse `self-update`.** On the scratch lake, now running the published v0.8.0 binary, run the published v0.6.0 agent's `self-update`. It should install v0.8.0, check it against the release checksums, and keep `terva-lampi.prev`.
6. **Notes.** Prepend the notes to both release bodies, with `####` headings.
7. **Land.** Land these ticket commits through a Forgejo PR. Deploying to the workstation is a separate ticket.

## Notes

**agent:claude-code/9078ac3f** at 2026-10-04T23:18:46Z

### Rehearsal, 2026-10-04

The new build was the release branch head 2d4a3af. Outside `.tickets`, it is main f150332 (#201's merge). The old binaries were the published v0.7.0 and v0.6.0 linux_amd64 archives, each checked against its release's `checksums.txt`. Everything ran with a scratch HOME and XDG on 127.0.0.1:18998, apart from the live lake and agent.

- **Seeded with v0.7.0.** The v0.7.0 binary created the lake (`catalog schema 21, created`), and the v0.6.0 agent uploaded one Claude session. It normalized: ready=1.
- **New agent, old lake.** The new agent's sync against the v0.7.0 lake read `unchanged 1`, uploading nothing.
- **Backup.** The lake was stopped and backed up with v0.7.0's `serve backup`. `serve fsck` on the copy checked 1 entry, 0 bad.
- **Upgraded.** `serve migrate --check` with the new build reported `catalog schema 21, up to date`, and serve logged the same at start, with no migration. The lake id was unchanged.
- **Both agents.** The v0.6.0 agent and the new agent each read `unchanged 1`, uploading nothing.
- **Checks.**
  - `/v1/stats`: 1 session, 1 artifact, normalization ready=1, failed=0, no jobs.
  - `serve normalize --status` agreed.
  - serve logged no warnings or errors.
- **MCP route.** This lake runs without a web config, so `/api/read/v1/mcp` answered 404 here. `mcpRoutes` mounts the route when the server has its registrations and event reader, and `serve` passes both whenever the web is configured.
  - Through the full lake handler with a web config, the endpoint answers 401 without a token and 404 for a raw-only token (`TestMCPReadsWithinTheTokenScope`, and `TestMCPBridgeLogsARefusedNotification` through `api.Server.Handler`).
  - The live v0.7.0 lake answers 404 to an anonymous POST there. The deploy script checks for 401 after the install.

The `self-update` rehearsal waits for the published release.

`GOFLAGS=-mod=mod just ci` passed on the release tree, and `just release-check` validated `.goreleaser.yaml`. GitHub CI passed on f150332.
