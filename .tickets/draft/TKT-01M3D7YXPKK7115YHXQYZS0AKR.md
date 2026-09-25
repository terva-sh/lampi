---
schema: 3
id: TKT-01M3D7YXPKK7115YHXQYZS0AKR
title: Mirror Forgejo lampi to GitHub and retire GitHub as the primary
type: chore
status: draft
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
claim: null
archive: null
created_at: 2026-09-25T21:36:21Z
updated_at: 2026-09-25T21:36:21Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

On 2026-09-25 the owner chose the internal Forgejo as lampi's primary forge: pull requests, review, and CI happen there, and GitHub (`github` remote, `terva-sh/lampi`) receives a push mirror. Before that, PRs #61 to #78 went through GitHub, and GitHub Actions was the only CI.

The in-tree half is done. `.forgejo/workflows/ci.yml` runs vet, gofmt, `go test -race`, and a build on the internal `golang:1.27-alpine` image, and the justfile and the GitHub workflow name it as the primary gate. What remains changes remote settings, and each item needs this ticket's authorization before an agent does it.

### State on 2026-09-25, read with `tea api` and `gh api`

- Forgejo `terva-sh/lampi`: Actions enabled, no push mirrors, no open pull requests, default branch `main`.
- Forgejo `main` held two ticket-filing commits (`ea6d047`, `e82a8f3`) that `github/main` at `705a2b7` did not.
- GitHub held 58 branches: `main` plus 57 `claude/*` and `cursor/*` branches left from merged or abandoned work.

### Work

- Add a push mirror on Forgejo `terva-sh/lampi` to `github.com/terva-sh/lampi`, with sync on commit. It needs a GitHub credential that can push. Record where the credential lives, and never its value.
- Decide whether the mirror pushes every branch or only `main`. Every branch keeps GitHub a full copy. Only `main` keeps agent branches off the public mirror.
- Confirm the first Forgejo CI run is green on the runner, since the workflow has not run there yet.
- Decide whether to delete the 57 stale GitHub branches. A mirror that pushes every branch does not delete branches that exist only on GitHub.
- Decide whether lampi takes the `terva-review.yml` workflow that other terva-sh repositories carry.

## Acceptance criteria

- [ ] A Forgejo push mirror updates GitHub main within one sync interval of a push
- [ ] The first Forgejo CI run on main is green
- [ ] The stale GitHub branches are deleted or kept, and the choice is recorded
