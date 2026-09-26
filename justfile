# lampi dev tasks. `just` lists them.
#
# These are the same steps .forgejo/workflows/ci.yml, the gate for
# internal pull requests, and .github/workflows/ci.yml, the gate for
# external agents' pull requests on GitHub, run. Both keep the
# commands inline because neither runner installs just. When you change
# a gate here, change it in both, and in the Makefile, which exists so
# `make test` works without just.

set shell := ["bash", "-eu", "-o", "pipefail", "-c"]

# `-buildvcs=false` inside a linked git worktree, empty otherwise.
# Go looks for a `.git` directory, and a linked worktree has a `.git`
# file, so `go build` stops with "error obtaining VCS status" before it
# compiles. `go test` does not link the command, so the suite can be
# green while the build is not.
buildvcs := `if [ "$(git rev-parse --git-dir 2>/dev/null)" != "$(git rev-parse --git-common-dir 2>/dev/null)" ]; then echo "-buildvcs=false"; fi`

# 0.0.0 means nothing was stamped. cmd's version string lives in internal/cli.
version := "0.0.0"
commit := `git rev-parse --short HEAD 2>/dev/null || echo unknown`
ldflags := "-s -w -X terva.sh/lampi/internal/cli.version=" + version + " -X terva.sh/lampi/internal/cli.commit=" + commit

default:
    @just --list

# Build bin/terva-lampi.
build:
    @mkdir -p bin
    go build {{buildvcs}} -trimpath -ldflags "{{ldflags}}" -o bin/terva-lampi ./cmd/terva-lampi
    @echo "built bin/terva-lampi ({{version}}, {{commit}})"

# Run the test suite. internal/accept is the MVP acceptance gate.
test:
    go test ./...

# go vet, including test files.
vet:
    go vet ./...

# Rewrite sources with gofmt.
fmt:
    gofmt -w .

# just hands each line to bash as written, so `$` is single here,
# unlike the Makefile.
# Fail if gofmt would change a file. CI runs this too.
fmt-check:
    @diff=$(gofmt -l .); \
    if [ -n "$diff" ]; then \
        echo "gofmt issues in:"; \
        echo "$diff"; \
        exit 1; \
    fi

# What CI runs, in order.
ci: vet fmt-check test build

# Build and tag terva-lampi:synthetic. Does not run tests and does not
# push. CI does not call this. The Makefile target is the same build,
# for a checkout without just.
synthetic-container:
    docker build -f e2e/Dockerfile -t terva-lampi:synthetic .

# Development runs, kept apart from a live lake and agent on the same
# machine. With no flags, serve writes to the XDG state dir and binds
# 127.0.0.1:8787, and the client commands read the real device token,
# machine id, server URL, and allowlist from the XDG config dir. The
# recipes below move all four: config and state under .dev/ in this
# checkout, the lake in .dev/lake, and the address to LAMPI_DEV_ADDR.
# XDG_CONFIG_HOME also moves the Cursor IDE and Cursor CLI defaults,
# so those two find nothing unless .dev/config sets their roots.
dev_dir := justfile_directory() / ".dev"
dev_addr := env("LAMPI_DEV_ADDR", "127.0.0.1:18787")
# Every path is quoted: the checkout path may hold a space.
dev_env := "XDG_CONFIG_HOME=" + quote(dev_dir / "config") + " XDG_STATE_HOME=" + quote(dev_dir / "state") + " LAMPI_SERVER=" + quote("http://" + dev_addr) + " LAMPI_TOKEN_FILE="

# Run a lake from .dev/lake on the dev address. Extra flags go to serve.
[positional-arguments]
dev-serve *args: build
    env {{dev_env}} bin/terva-lampi serve --data {{quote(dev_dir / "lake")}} --addr {{quote(dev_addr)}} "$@"

# `just dev sync`, `just dev status`, `just dev agent discover`.
# Run any command with .dev config and state, against the dev lake.
[positional-arguments]
dev *args: build
    env {{dev_env}} bin/terva-lampi "$@"

# Remove .dev/: the dev lake, machine id, token, and agent state.
dev-clean:
    rm -rf {{quote(dev_dir)}}

# Internal work merges on Forgejo (origin) and external agents' pull
# requests merge on GitHub (github), so each main can move ahead of the
# other. This fast-forwards whichever is behind. When both have moved
# it stops and names the heads: merge them on a branch, open a Forgejo
# PR, and run this again after it lands. It never force-pushes, and it
# prints the plan and pushes nothing without --yes. docs/pr-reviews.md.
# Fast-forward the stale main between Forgejo and GitHub. Needs --yes.
[positional-arguments]
sync-github *flags:
    #!/usr/bin/env bash
    set -euo pipefail
    yes=false
    for f in "$@"; do
        case "$f" in
            --yes) yes=true ;;
            *) echo "sync-github: unknown flag $f" >&2; exit 2 ;;
        esac
    done
    git fetch --quiet origin main
    git fetch --quiet github main
    forgejo=$(git rev-parse origin/main)
    github=$(git rev-parse github/main)
    if [ "$forgejo" = "$github" ]; then
        echo "in sync at ${forgejo:0:12}"
        exit 0
    fi
    if git merge-base --is-ancestor "$github" "$forgejo"; then
        from=origin; to=github; head=$forgejo
    elif git merge-base --is-ancestor "$forgejo" "$github"; then
        from=github; to=origin; head=$github
    else
        echo "diverged: origin/main ${forgejo:0:12}, github/main ${github:0:12}" >&2
        echo "merge them on a branch and land it through a Forgejo PR" >&2
        exit 1
    fi
    echo "fast-forward $to/main to $from/main ${head:0:12}:"
    git log --oneline "$to/main..$head"
    if [ "$yes" != true ]; then
        echo "dry run; pass --yes to push"
        exit 0
    fi
    git push "$to" "$head:refs/heads/main"
