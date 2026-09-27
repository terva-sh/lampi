---
schema: 3
id: TKT-01M3HS0FX3X8PKP69GWPE6GYM0
title: Release archives on GitHub and a curl-able install.sh
type: task
status: done
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
created_at: 2026-09-27T15:51:16Z
updated_at: 2026-09-27T17:39:07Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Installing terva-lampi on a new machine today means building from a checkout or copying a binary by hand. The owner wants a script they can curl and run, published the way the rest of the terva-sh org publishes: goreleaser archives with `checksums.txt` on GitHub releases, and an `install.sh` at the repository root.

### Approach

Follow git-ticket's release setup:

- `.goreleaser.yaml`: linux and darwin on amd64 and arm64, windows amd64. CGO off, since the SQLite driver is pure Go. Archives named `terva-lampi_VERSION_OS_ARCH`, with LICENSE and README.
- `.github/workflows/release.yml`: goreleaser publishes on a `v*` tag, guarded to github.com.
- `.forgejo/workflows/release.yml`: builds with publishing skipped, then uploads through the org bot token.
- `install.sh`: POSIX sh, reads the latest tag or `--version`, verifies sha256 before unpacking, never sudos, and installs into `--prefix` or `~/.local/bin`.

The only lampi-specific addition is `--register`, which runs `register --install-service`. Under `curl | sh`, stdin is the script, and `register` reads the code from stdin when it is not a terminal. So the installer runs it with stdin and stdout on `/dev/tty`, and refuses when there is no terminal.

`--version` falls back to `runtime/debug` build info when nothing was stamped. A goreleaser build then reports its tag without link-time flags, which is the org's rule, and `just build` keeps its ldflags.

Alternatives: a lake-served installer, rejected by the owner in favour of the org pattern; per-platform raw binaries without archives, rejected because the org publishes archives plus `checksums.txt`.

## Acceptance criteria

- [x] A v* tag publishes goreleaser archives and checksums.txt on GitHub
- [x] install.sh verifies sha256 before unpacking and installs without sudo
- [x] install.sh --register runs register on the terminal, never on the piped script
- [x] A release binary's --version carries its tag
- [x] README documents install and the release steps

## Implementation plan

Copy git-ticket's release setup and adapt it to terva-lampi:

- `.goreleaser.yaml`: five targets and `checksums.txt`.
- Both release workflows: the GitHub one guarded to github.com, the Forgejo one publishing with `BOT_TOKEN`.
- `install.sh` at the root.
- A `runtime/debug` fallback in `versionLine`, so a tagged build names its tag without `-X`.
- `just release-check` and `just release-snapshot`.
- Docs in the README, `docs/development.md#releases` and `docs/registration-and-lakes.md`.

`install.sh --register` hands register `/dev/tty` and refuses up front with no terminal. The tests serve a fake release over httptest and run the script with no controlling terminal (setsid).

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-27T15:55:33Z

### Verification so far

- `goreleaser check` passes. `goreleaser release --snapshot` built all five archives, each with LICENSE, README.md and the binary at the root.
- A scratch clone tagged v0.1.0 locally, never pushed, built a binary that answers `terva-lampi v0.1.0 (14f580b4656b)`.
- Under a pseudo-terminal, `cat install.sh | sh -s -- --register --lake work` gave register a terminal on stdin with the right arguments.
- The TestInstallScript* suite covers the latest-release install, a pinned version and prefix, a bad checksum, an asset missing from checksums.txt, --register with no terminal (refused before any request), and --lake without --register.

### Not claimed

`go install terva.sh/lampi/...` does not resolve: the vanity host serves only terva.sh/terva. So the README does not offer it. Fixing that is outside this repository.

**agent:claude-code/e4a47e8c** at 2026-09-27T16:09:17Z

### Review rounds on PR #25

**Round 1** (review 1002):

- High, "tagged builds report 0.0.0": rejected. Since Go 1.24, `go build` at a tag stamps `Main.Version`. A real tagged-build test was added, `TestTaggedBuildReportsItsTag`, which clones, tags, builds and checks `--version`.
- Two installer findings, fixed in c7eb34d:
  - The staged binary is now run before it replaces the old one.
  - A partial copy is removed.
- CI failed because the Alpine CI image has no curl. curl was added to the Forgejo CI dependencies, and the tests skip without it.

**Round 2** (review 1003): the Forgejo publish was not resumable after a partial upload. Fixed in 28930b8: a rerun reuses the tag's release and skips assets already uploaded. git-ticket's workflow has the same gap.

**Round 3** (review for e30fa5ce): "verify before publishing on GitHub" was rejected. It is the org pattern, and a bad tag is now caught before tagging by `TestTaggedBuildReportsItsTag`.

CI on 28930b8 failed once without a readable log: this Forgejo returns 404 for job logs over the API. The whole race suite passed in the CI image from a depth-1 clone of that commit.

**agent:claude-code/e4a47e8c** at 2026-09-27T17:25:03Z

### v0.1.0 tagged, not released

v0.1.0 was tagged on 5a58468 and pushed to both forges. Both release jobs failed and nothing was published:

- **GitHub.** `TestTaggedBuildReportsItsTag` failed in the Test step. At a real tag, HEAD carried both v0.1.0 and the test's own tag, and go build took the higher one. The test was wrong: the binary reported `terva-lampi v0.1.0 (5a58468ebd4a)`.
- **Forgejo.** goreleaser built every archive and the version check passed. The publish step's new existing-release lookup got an HTML 404, which jq could not parse.

Fixed on release/fix-first-release:

- The test commits in its clone before tagging.
- The lookup uses `curl -f`.

The owner chose to leave v0.1.0 in place and release v0.1.1 rather than rewrite a pushed tag.

## Summary

v0.1.1 is the first published release: tag on 53d1c54, released 2026-09-27.

- **GitHub:** the release carries the linux and darwin amd64 and arm64 archives, the windows amd64 zip, and `checksums.txt`. Forgejo has the same six assets.
- **Installer check:** `curl -fsSL https://raw.githubusercontent.com/terva-sh/lampi/main/install.sh | sh` into a fresh HOME downloaded v0.1.1, verified its sha256, and installed a binary reporting `terva-lampi v0.1.1 (53d1c54f89c3)`.
- **Earlier failed tag:** v0.1.0 stays on 5a58468 with no release. Both of its jobs failed on bugs fixed in PR #27. The owner chose v0.1.1 over rewriting the pushed tag.

Landed in PRs #25 and #27.

Release procedure: `docs/development.md#releases`.
