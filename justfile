# lampi dev tasks. `just` lists them.
#
# These are the same steps .forgejo/workflows/ci.yml, the primary gate,
# and .github/workflows/ci.yml, on the GitHub mirror, run. Both keep the
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
dev_env := "XDG_CONFIG_HOME=" + dev_dir / "config" + " XDG_STATE_HOME=" + dev_dir / "state" + " LAMPI_SERVER=http://" + dev_addr + " LAMPI_TOKEN_FILE="

# Run a lake from .dev/lake on the dev address. Extra flags go to serve.
[positional-arguments]
dev-serve *args: build
    env {{dev_env}} bin/terva-lampi serve --data {{dev_dir}}/lake --addr {{dev_addr}} "$@"

# `just dev sync`, `just dev status`, `just dev agent discover`.
# Run any command with .dev config and state, against the dev lake.
[positional-arguments]
dev *args: build
    env {{dev_env}} bin/terva-lampi "$@"

# Remove .dev/: the dev lake, machine id, token, and agent state.
dev-clean:
    rm -rf {{dev_dir}}
