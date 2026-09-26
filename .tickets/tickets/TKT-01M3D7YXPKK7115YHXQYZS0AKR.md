---
schema: 3
id: TKT-01M3D7YXPKK7115YHXQYZS0AKR
title: "Two forges: Forgejo for internal work, GitHub for external agents"
type: chore
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
  - area/ci
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/cd41c9ac
  branch: t3code/repository-orientation-setup
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-cd41c9ac
  commit: 60b249f7b77087727017f4efa21465a8a129192b
  session: null
  claimed_at: 2026-09-25T21:44:46Z
  expires_at: null
archive: null
created_at: 2026-09-25T21:36:21Z
updated_at: 2026-09-25T22:02:14Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

On 2026-09-25 the owner chose the internal Forgejo as lampi's primary forge for internal work: pull requests, `terva-review`, and CI. GitHub (`github` remote, `terva-sh/lampi`) stays the public copy, and external agents watch it and open their pull requests there. So lampi has two review processes, one on each forge, and they meet at `main`.

### Decisions, 2026-09-26, by the owner (human:sothr) in a working session

- **No Forgejo push mirror.** A push mirror force-pushes. A pull request from an external agent merged on GitHub would be overwritten at the next sync. Its commit would survive only on the closed pull request page. The mirror also fits nothing else in the org: no terva-sh repository has a push mirror, and `meta` decision 0001 says no public mirror is configured.
- **`main` is synced by hand, fast-forward only.** `just sync-github` fetches both remotes and fast-forwards whichever `main` is behind. When the two have diverged, it stops and names both heads, so a person merges them. It never force-pushes. The alternatives considered were a main-only push mirror, which would make GitHub read-only for external agents, and GitHub as the primary for `main`, which would put internal review behind the public forge. Both lost to keeping each forge's own merge process and never rewriting either `main`.
- **The 57 leftover GitHub branches are deleted.** Each belongs to a pull request that GitHub reports as MERGED (squash merges, so none is an ancestor of `main`, and the work is in `main`). The pull request pages keep their diffs. The owner authorized deleting them in this ticket.
- **lampi adopts `terva-review`** for internal Forgejo pull requests. It uses the `single-review.yml` example from `terva-action-code-review` at the v0.3.0 image digest, with `warricksothr` as the maintainer. It reads the org-level `BOT_TOKEN` and `CPA_API_KEY` secrets and the `TERVA_REVIEW_*` org variables, all of which exist. External pull requests on GitHub are reviewed through GitHub's process and are not covered by this workflow.

### State on 2026-09-25, read with `tea api` and `gh api`

- Forgejo `terva-sh/lampi`: Actions enabled, no push mirrors, no open pull requests.
- Forgejo `main` held two ticket-filing commits (`ea6d047`, `e82a8f3`) that `github/main` at `705a2b7` did not.
- GitHub held 58 branches: `main` plus 57 `claude/*` and `cursor/*` branches, all belonging to merged pull requests.

## Acceptance criteria

- [ ] just sync-github fast-forwards the stale main and refuses when the two have diverged
- [x] The 57 merged GitHub branches are deleted and only main remains
- [x] .forgejo/workflows/terva-review.yml is installed at the v0.3.0 image digest
- [x] The first Forgejo CI run on a lampi PR is green
- [x] docs/pr-reviews.md describes both review processes and the main sync

## Notes

**agent:claude-code/cd41c9ac** at 2026-09-25T21:47:24Z

Public tree, decided by the owner on 2026-09-26. GitHub `main` held no internal hostnames before this change. The two `.forgejo` workflows name `container.local.sothr.com` as their image registry, and they reach GitHub with the next sync. The owner accepted that, as git-ticket did in its TKT-01M1FAFS. The reference is working CI configuration, and the host is not reachable from outside. A filtered publish that kept `.forgejo/` off GitHub lost: `main` could then no longer be identical on both forges, and the fast-forward model depends on that.

On the owner's instruction, the host name `brokkr` in TKT-01M3D57Q (sqlitesnap misses a checkpoint within one mtime tick) was reworded to "the owner's workstation". The original wording stays in the history of `ea6d047`, which a fast-forward sync publishes. Removing it would need a history rewrite and a force-push, and neither was done.

**agent:claude-code/cd41c9ac** at 2026-09-25T21:48:30Z

Deleted the 57 GitHub branches on 2026-09-26 with `gh api -X DELETE .../git/refs/heads/NAME`. Only `main` remains. Each deletion was checked first against `gh pr list --state merged`:

- 55 branch heads equalled the head their pull request merged at.
- `cursor/agent-daemon-loop-e44f` carried one commit past PR #5, `edc78c1`, "Do not start a backoff retry after the agent is cancelled".
- `cursor/cursor-1d-composer-usage-d660` carried one commit past PR #57, `3a57d77`, "lock the QE usage and compaction fixture matrix".

Both extra commits match a commit already on `github/main` by `git patch-id --stable`, so no work was lost.

`just sync-github` was tested in a throwaway repository with two bare remotes. It reported equal heads as in sync. With GitHub ahead, it fast-forwarded Forgejo on `--yes`. With the two diverged, it stopped with exit 1 and named both heads. A dry run against the real remotes showed Forgejo two commits ahead (`ea6d047`, `e82a8f3`). Criterion 1 stays open until a real sync has run.

**agent:claude-code/cd41c9ac** at 2026-09-25T21:52:58Z

First Forgejo runs on PR #1 at head `144d9a5`.

**CI (run 4) failed in `go test -race`.** Four hook tests in `internal/cli/packaging_test.go` failed with "agent pid N is not running". `startCopiedSleep` copies `sleep` under the name `terva-lampi`. On `golang:1.27-alpine`, `sleep` is busybox, which picks its applet from the program name, so the copy exits 127 at once and reads as a dead agent. A local run of a renamed busybox gave "applet not found". This is not a defect in the hook. The workflow now installs `coreutils`, so `sleep` is a standalone binary. GitHub CI on ubuntu never hit this.

**terva-review (run 5, review 747) reported two findings.** Both were accepted and fixed.

- High: `dev-clean` ran `rm -rf {{dev_dir}}` unquoted, so a checkout path with a space would split and could delete the wrong directory. The Makefile had the same problem. Every dev path is now quoted, with just's `quote()` and with double quotes in make. Tested in a scratch checkout at `.../my work/` with a decoy at `.../my/`: only `.dev` was removed, under both just and make.
- Medium: `sync-github` put its flags into the script source, so `$(...)` would run before validation. The recipe is now `[positional-arguments]` and loops over `"$@"`. Tested with `'$(touch /tmp/...)'`: it was rejected as an unknown flag, and the file was not created.

**agent:claude-code/cd41c9ac** at 2026-09-25T21:58:36Z

Correction to the earlier note on CI run 4: installing `coreutils` did not fix the hook tests (run 6 failed the same way). Alpine builds coreutils as a single multi-call binary as well, so a renamed copy of `sleep` still picked its applet from the name. The same was true of the test that rewrites argv[0]. The fix is now in the tests, not the image. With `LAMPI_TEST_HOOK_STAND_IN` set, the test binary is the stand-in agent (`internal/cli/standin_unix_test.go`). It catches SIGUSR1 and prints `ready`, and the tests wait for that line, because a Go process drops SIGUSR1 until it asks for it. Checked with busybox `sleep` first in PATH, and with `-race -count=5`. The `coreutils` line has been removed from the workflow.

**agent:claude-code/cd41c9ac** at 2026-09-25T22:02:14Z

PR #1 at c5f902f: Forgejo CI run green (vet, gofmt, go test -race, build); terva-review clean (run 9), with review 749's finding read as resolved by the new sqlitesnap test. Dispositions for reviews 747 and 749 are for the maintainer to post; an agent does not post them under the owner's account.
