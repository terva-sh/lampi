---
schema: 3
id: TKT-01M3B3698MKV82QSR0AR2CQYNS
title: "Agent privacy: git exec, remote userinfo, path keys, deny rules"
type: bug
status: ready
status_reason: null
priority: urgent
due_on: null
labels:
  - area/agent
  - area/adapter
  - policy
assignees: []
milestone: null
parent: TKT-01M3B35JS8J2F83FG4J7ZC0199
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-25T01:34:31Z
updated_at: 2026-09-25T01:38:33Z
created_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
updated_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
extensions: {}
---

## Description

Four agent defects can send bytes, or run code, that the allowlist should have stopped.

### Findings

- `git` runs under the repository's own config. Proven on git 2.43. `ProjectAt` runs for every discovered session, before the allowlist. `rootGit` (`internal/adapter/git.go:363-395`) drops the global and system config, but a checkout's `.git/config` still applies. A hostile checkout can make that `git` invocation run a command as the agent user. It is enough to have run one agent session in that directory.
- Remote credentials go to the lake. Proven. `originURL` (`git.go:126-148`) returns the URL raw, so `https://user:token@host/…` lands in `manifest.project.git_remote`, the catalog, and every refusal line. The manifest is never scanned.
- One session can upload another's bytes. Proven. `Bundle.Paths` is keyed by content digest in every adapter (for example `adapter/terva/terva.go:223`) and read at `internal/upload/prepare.go:126-130`. Two files identical at hash time, such as empty `.errors.jsonl` sidecars, map to one path. If the other one changes before the read, the allowed session uploads the denied session's bytes.
- Deny fails open. Proven. `internal/config/policy.go:44-95` (`Permitted`, `matches`, `cwdHasPrefix`). A `cwd_prefix` deny misses a case variant on APFS, a symlinked cwd bypasses it, and a `git_remote` deny misses a session whose remote reads empty.

### Approach

Resolve the project only for a cwd that already passed a `cwd_prefix` or `cwd_hash` rule, or a `git_remote` rule's cheap pre-check. Where git still runs, pin config overrides that disable protocols, hooks, fsmonitor, and lazy fetch. Strip userinfo in `originURL`, and scan the manifest JSON before the POST. Key `Paths` by relpath and check that the bytes read hash to the artifact digest. Fold case on darwin and Windows, also test `EvalSymlinks(cwd)`, and have a `git_remote` deny refuse a session whose remote is unknown.

## Acceptance criteria

- [ ] A checkout's own git config cannot run a command during project resolution; a test proves it
- [ ] git_remote never carries userinfo, and manifests are scanned before POST
- [ ] A session can only upload bytes read from its own paths that hash to its digests
- [ ] Deny rules hold across case, symlinks, and an unknown remote
