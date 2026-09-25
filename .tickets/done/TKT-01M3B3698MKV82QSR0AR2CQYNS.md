---
schema: 3
id: TKT-01M3B3698MKV82QSR0AR2CQYNS
title: "Agent privacy: git exec, remote userinfo, path keys, deny rules"
type: bug
status: done
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
updated_at: 2026-09-25T02:04:02Z
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

- [x] A checkout's own git config cannot run a command during project resolution; a test proves it
- [x] git_remote never carries userinfo, and manifests are scanned before POST
- [x] A session can only upload bytes read from its own paths that hash to its digests
- [x] Deny rules hold across case, symlinks, and an unknown remote

## Implementation plan

### git exec

`ProjectAt` stops running git. It reads the remote, HEAD, and the root commit from files only: loose objects as before, plus a new pack reader (idx v2 and pack, with ofs and ref deltas, bounded sizes and depth). A new `adapter.ResolveRoot` runs `git rev-list` only when the in-process walk found no root, and `upload.prepare` calls it only after `Permitted` admitted the session. Every allow and deny field (cwd, cwd hash, remote) is a file read, so the allowlist never needs git. The exec keeps the parent environment dropped and adds `--git-dir` of the repository already read, `-c` overrides for protocol, hooks, fsmonitor, ssh, and proxy, and the env `GIT_ALLOW_PROTOCOL=` (overrides per-protocol config, which `protocol.allow=never` does not), `GIT_NO_LAZY_FETCH=1`, `GIT_PROTOCOL_FROM_USER=0`, `GIT_NO_REPLACE_OBJECTS=1`, `GIT_OPTIONAL_LOCKS=0`. A regression test builds a partial-clone checkout whose promisor remote uses the ext transport and whose HEAD is missing, calls `ProjectAt` and `ResolveRoot`, and checks no marker file appears.

### Remote userinfo and the manifest scan

`originURL` strips userinfo from a URL with a scheme and keeps the scp `user@host:` form unless that user part carries a `:`. `prepareBundle` marshals the manifest after the artifact scan and runs the redact ruleset over it. A hit refuses the session, appends a quarantine record (rule names and the manifest digest, not the match), and is not subject to `upload_hits`.

### Paths

`Bundle.Paths` is keyed by artifact relpath in every adapter. `scanSession` reads `Paths[RelPath]`, and a body whose sha256 differs from the artifact digest skips the session this round without enqueueing. The watcher's event for that write kicks the next round.

### Deny

Deny rules fold case on every OS (Linux also mounts case-insensitive filesystems) and match the raw cwd or its `EvalSymlinks` form against the raw or resolved prefix; `cwd_hash` deny also checks the hash of the resolved cwd. Allow stays exact, so a case or symlink variant is refused. A deny rule's `git_remote` matches a session whose remote is unknown. `ProjectID.NoRepo`, set by the caller from `adapter.OutsideCheckout`, marks a cwd that exists and that no repository contains; only that empty remote counts as known. The zero value is unknown, so a caller that does not check fails closed.

## Notes

**agent:claude-code/eh1m** at 2026-09-25T02:03:44Z

### Decisions that differ from the approach

- The allowlist never needs git: cwd, cwd hash, and origin are file reads. So instead of a pre-check per rule type, `ProjectAt` never runs git at all, and `upload.prepare` calls the new `adapter.ResolveRoot` only after `Permitted`.
- The in-process reader now walks version 2 packs (offset and ref deltas, bounded depth, sizes, and a per-walk inflate budget; FIFOs and devices are not opened). Real clones resolve without git, so the exec is a rare fallback. Found roots are cached per common dir and HEAD.
- Measured on git 2.43: `-c protocol.allow=never` alone does NOT stop the exploit, because the checkout's `protocol.ext.allow=always` is more specific. `GIT_ALLOW_PROTOCOL=` (empty) overrides per-protocol config, and `GIT_NO_LAZY_FETCH=1` also works on this 2.43 build. Each blocks it alone; the test asserts the protocol block holds without the lazy-fetch switch. git also runs with `--git-dir` of the repository already read and the commit id, not HEAD.
- Userinfo: every URL scheme loses the whole userinfo except ssh (and git+ssh), which keeps a bare login name and loses a password. scp form is unchanged, since git ends the host at the first colon and the part before `@` there is a login name.
- Manifest hits are refused even with `upload_hits`, since a manifest has no redaction stamp. The quarantine record and the refusal line carry the relpath passed through `redact.Ruleset{}.Strip`, and no cwd, because the match can be in either.
- Deny folds case on every OS, not only darwin and Windows: Linux also mounts case-insensitive filesystems, and over-denying a case-variant sibling is the safe direction. Allow stays exact.
- `git_remote` deny semantics: the field matches when the remote is unknown. `config.ProjectID.NoRepo` (zero value unknown, so a caller that does not set it fails closed) is true when `adapter.OutsideCheckout` saw an existing cwd with no `.git` up the tree; that empty remote is known and does not match. This keeps a `git_remote`-only deny from refusing every scratch directory, while a deleted cwd or a checkout without origin is refused. A `cwd_prefix` on the rule scopes it. `export --format sharegpt` sets `NoRepo` the same way.
- A changed file skips the whole session this round (`errFileChanged`), with no error. The watcher's event for that write kicks the next sync.

### Also fixed

- A relative alternate was joined to the git dir rather than `objects/`.

### Follow-ups, not done

- APFS is also normalization-insensitive (NFC vs NFD); deny folds case but does not normalize Unicode.
- `originURL`, `headCommit`, and `packed-refs` still use `os.ReadFile`, which blocks on a FIFO planted in a checkout (hang, not exec). Pre-existing.
- Commit-graph and multi-pack-index are not read; history past 100000 first-parent commits or 256 MiB of inflated pack data falls back to git after the allowlist.

## Summary

Landed on `claude/elegant-feynman-eh1mdd-privacy` in commit c15ad38.

- `ProjectAt` reads files only, now including version 2 packs with offset and ref deltas (`internal/adapter/gitpack.go`). `ResolveRoot` runs a hardened `git rev-list` only for a session `Permitted` admitted. `TestProjectResolutionIgnoresCheckoutCommands` builds a partial clone with an ext-transport promisor remote and a missing HEAD, proves the payload fires under plain git, and asserts it does not fire through resolution.
- `originURL` strips userinfo (ssh keeps a bare login). `prepareBundle` scans the manifest JSON and quarantines a hit with rule names and a stripped relpath.
- `Bundle.Paths` is keyed by relpath in all six adapters, and `readArtifacts` skips a session whose bytes no longer hash to the digest.
- Deny rules fold case, try symlink-resolved cwd and prefix, match the resolved cwd hash, and treat an unknown remote as a `git_remote` match. `ProjectID.NoRepo` marks a known-empty remote.
- README "Off-box raw", `docs/policy.md`, and `docs/protocol.md` describe the new semantics.
