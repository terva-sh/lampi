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
updated_at: 2026-09-25T21:47:24Z
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
- [ ] The 57 merged GitHub branches are deleted and only main remains
- [ ] .forgejo/workflows/terva-review.yml is installed at the v0.3.0 image digest
- [ ] The first Forgejo CI run on a lampi PR is green
- [ ] docs/pr-reviews.md describes both review processes and the main sync

## Notes

**agent:claude-code/cd41c9ac** at 2026-09-25T21:47:24Z

Public tree, decided by the owner on 2026-09-26. GitHub `main` held no internal hostnames before this change. The two `.forgejo` workflows name `container.local.sothr.com` as their image registry, and they reach GitHub with the next sync. The owner accepted that, as git-ticket did in its TKT-01M1FAFS. The reference is working CI configuration, and the host is not reachable from outside. A filtered publish that kept `.forgejo/` off GitHub lost: `main` could then no longer be identical on both forges, and the fast-forward model depends on that.

On the owner's instruction, the host name `brokkr` in TKT-01M3D57Q (sqlitesnap misses a checkpoint within one mtime tick) was reworded to "the owner's workstation". The original wording stays in the history of `ea6d047`, which a fast-forward sync publishes. Removing it would need a history rewrite and a force-push, and neither was done.
