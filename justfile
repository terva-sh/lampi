# lampi dev tasks. `just` lists them.
#
# These are the same steps .github/workflows/ci.yml runs. The workflow
# keeps the commands inline: the runner is setup-go on ubuntu and does
# not install just. When you change a gate here, change it there, and
# in the Makefile, which exists so `make test` works without just.

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

# Fail if gofmt would change a file. CI runs this too.
fmt-check:
    @diff=$$(gofmt -l .); \
    if [ -n "$$diff" ]; then \
        echo "gofmt issues in:"; \
        echo "$$diff"; \
        exit 1; \
    fi

# What CI runs, in order.
ci: vet fmt-check test build

# Build and tag the local synthetic smoke image (terva-lampi:synthetic).
# Does not run the suite and does not push. CI does not call this.
# The Makefile target is the same build, for a checkout without just.
synthetic-container:
    docker build -t terva-lampi:synthetic -f e2e/Dockerfile \
        --build-arg VERSION={{version}} \
        --build-arg COMMIT={{commit}} \
        .
