---
schema: 3
id: TKT-01M424P3QSYP18KFCQWQ2TA2TK
title: "Release v0.7.0: Grok Bot harness; tag and notes"
type: task
status: done
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
claim: null
archive: null
created_at: 2026-10-04T00:23:10Z
updated_at: 2026-10-04T00:33:34Z
created_by:
  id: agent:claude-code/5949504c
  name: ""
updated_by:
  id: agent:claude-code/5949504c
  name: ""
extensions: {}
---

## Description

The owner asked on 2026-10-03 to cut a release after v0.6.0 carrying the Grok Bot harness. That request is this ticket's promotion and confirms the cut. The owner asked whether the bump should be minor given no breaking change; it is v0.7.0, because the repository bumps the minor for a new feature (v0.6.0 added Grok Build) and the patch only for fixes (v0.5.1, v0.5.2). A patch bump would tell operators the release is fixes only.

### What it carries

Since v0.6.0 (6b3a9d9), main gained one change: **#83** (Add the grokbot harness for Grok Bot transcripts), merged on GitHub and fast-forwarded onto Forgejo main with `just sync-github --yes`. main on both forges is 478435f.

- A new `grokbot` harness captures `agent-transcripts/<uuid>/<uuid>.jsonl` under `GROK_BOT_HOME`, or `harnesses.grokbot.root` when set. There is no default home, so an agent captures Grok Bot only when one of the two is set.
- A new `grokbot` normalizer projects user and assistant text onto schema_version 1. Session ids are `grokbot:<uuid>`.
- The lake accepts `harness: grokbot` in manifests (`knownHarnesses`, internal/api/merge.go).

`git diff v0.6.0 -- internal/catalog/catalog.go` is empty. In `internal/normalize` the diff adds `grokbot.go` and tests only, so no existing normalizer changes what it writes. `capture_protocol` is unchanged.

- The catalog schema does not change, and no migration is appended.
- Sessions already normalized are not queued again.
- **Upgrade order:** a lake older than v0.7.0 refuses a `grokbot` manifest with `harness "grokbot" is not supported`. Upgrade the lake before any agent that sets a Grok Bot home. Other harnesses are unaffected.
- **Rollback** is a binary swap. A lake rolled back keeps its `grokbot` sessions but refuses new ones.

Alternatives considered: v0.6.1, rejected because a new harness is a feature and the repository's patch releases have carried fixes only.

## Acceptance criteria

- [x] A lake at v0.6.0 starts on a build of the final main with nothing to migrate, accepts and normalizes a grokbot session, and passes health and fsck
- [x] The release is tagged on both forges and its archives and image name the tag
- [x] Release notes say there is no migration, rollback is a binary swap, the lake upgrades before agents that capture Grok Bot, and describe the harness

## Implementation plan

1. Rehearse on 478435f: a scratch lake seeded by the v0.6.0 release binary, confirm it refuses a grokbot manifest, then start the new build on it, upload and normalize a grokbot session, check migrate --check, health, normalize --status and fsck. Run GOFLAGS=-mod=mod just ci and just release-check.
2. Tag v0.7.0 (annotated) at 478435f, main on both forges, push to origin and github, check archives and checksums on both forges, the GHCR image's --version, and prepend the notes to both release bodies with #### headings.
3. Land these ticket commits through a Forgejo PR. Deploy to the internal lake only when the owner asks, as a separate ticket.

## Notes

**agent:claude-code/5949504c** at 2026-10-04T00:27:24Z

### Rehearsal, 2026-10-03

The new build was the release branch head 0f23df5, which is main 478435f plus this ticket's commit. The old binary was the published v0.6.0 linux_amd64 archive, checked against its `checksums.txt`. Everything ran with a scratch HOME, XDG and token on 127.0.0.1:18997, apart from the live lake and agent.

- **Seeded with v0.6.0.** The v0.6.0 binary created the lake (`catalog schema 21, created`), and the v0.6.0 agent uploaded one Claude session.
- **New agent, old lake.** The new agent against the v0.6.0 lake got `POST /v1/manifests: 400 Bad Request: {"error":"harness \"grokbot\" is not supported"}` for the Grok Bot session. Its blob was stored, the manifest was not, and the Claude session stayed unchanged. This is the upgrade-order note.
- **Upgraded.** `serve migrate --check` with the new build reported `catalog schema 21, up to date`, and serve logged the same at start, with no migration.
- **Grok Bot accepted.** The next sync posted the Grok Bot manifest (manifests 1, uploaded 0, because the blob was already there). The sync after that uploaded nothing (unchanged 2).
- **Checks.** `/v1/stats`: 2 sessions, 2 artifacts, normalization ready=2, failed=0, no jobs. `serve fsck` checked 2 entries, 0 bad. serve logged no warnings or errors.

`GOFLAGS=-mod=mod just ci` passed on the release tree, and `just release-check` validated `.goreleaser.yaml`. GitHub CI passed on 478435f (run 37087404243).

**agent:claude-code/5949504c** at 2026-10-04T00:33:33Z

### Published, 2026-10-03

- **Tag.** v0.7.0 is an annotated tag on 478435f, which is main on both forges. It was pushed to origin and github.
- **Workflows.** The GitHub release workflow (run 37165014225) passed. On Forgejo, Build Image and Build and Publish Release passed (release id 17715).
- **Archives.** On each forge, all five archives match that forge's `checksums.txt`, and the release is neither a draft nor a prerelease. Both linux_amd64 binaries print `terva-lampi v0.7.0 (478435f193ed)`.
- **Image.** With podman, `ghcr.io/terva-sh/lampi:0.7.0` and `:latest`, freshly pulled, both print `terva-lampi v0.7.0 (478435f)`.
- **Notes.** The notes were prepended to both release bodies, with `####` headings.

### Upgrade-order finding

Supersedes the line in the rehearsal note that only covered the refused manifest. A second rehearsal on a fresh v0.6.0 lake, with one new Claude session and one Grok Bot session, showed that a refused manifest fails the whole pass: `sync` exits 1. Both Claude sessions still uploaded, because Grok Bot is last in `knownSources` and is posted after every other harness. The notes say this, and the running agent's backoff of up to 5 minutes comes from docs/agent.md. Filed as TKT-01M4250SZ25TKAD2ZHE6APJFVS (Agent: one refused manifest fails the whole sync pass). #83 also shipped without operator docs, filed as TKT-01M4250T0FQD7QKTDR26ZGNYX6 (Docs: document the grokbot harness). Both are drafts.

Deploying to the internal lake waits for the owner to ask.

## Summary

v0.7.0 is tagged at 478435f and published on both forges with notes. The archives and the GHCR image (0.7.0 and latest) report v0.7.0. It carries #83, the grokbot harness: opt-in capture under GROK_BOT_HOME or harnesses.grokbot.root, normalized to user and assistant text. There is no migration (schema 21), and rollback is a binary swap. Upgrade the lake before any agent that sets a Grok Bot home: an older lake refuses grokbot manifests, which fails the agent's pass after the other harnesses have posted. Follow-ups: TKT-01M4250SZ2 and TKT-01M4250T0F, both drafts. Deploying is not part of this ticket.
