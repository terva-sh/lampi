# Developing lampi

Read this to build and test `terva-lampi` from a checkout, to run it
without touching a live lake on the same machine, and to track work in
the ticket store. How pull requests are reviewed and merged is in
[Pull requests and reviews](pr-reviews.md). Back to the
[documentation index](README.md).

## Build and test

You need Go 1.27. `just` is optional; the Makefile repeats every target
for a machine without it.

```bash
make test           # go test ./...
make build          # bin/terva-lampi
just ci             # vet, gofmt, test, build: what CI runs
```

CI on Forgejo and on GitHub runs `go vet`, `gofmt -l`, `go test ./...`,
and a build. Sibling terva-sh repositories use a justfile rather than
golangci-lint. When you change a gate in the justfile, change it in
both workflow files and in the Makefile too.

The module is `terva.sh/lampi`, the same vanity prefix as
`terva.sh/terva`. To build by hand:

```bash
go build -o bin/terva-lampi ./cmd/terva-lampi
```

Inside a linked git worktree, `go build` needs `-buildvcs=false`.
`just build` adds it for you.

The dashboard uses embedded Go templates and assets, so `make build`
produces everything. There is no frontend build.

### The MVP acceptance gate

`go test ./...` includes `internal/accept`, the MVP gate. That package
is `TestMVPAcceptance`: one fixture terva session against a local lake.
It checks that:

- ingest records a session uid and blob sha256;
- a second sync uploads nothing;
- an append uploads only the new tail and moves the head;
- a copy of that file from a second machine is a CAS hit, with one
  provenance row per machine;
- a sqlite query of `terva-lampi export` finds the fixture prompt.

See [Architecture](architecture.md#mvp-acceptance-gate).

### Browser, container, and go-live tests

The Playwright dashboard smoke, the synthetic container image, and the
opt-in go-live drills are in [e2e/README.md](../e2e/README.md).

### The lake image

`Dockerfile` at the repository root builds the image that runs `serve`,
for `linux/amd64` and `linux/arm64`. `just image` builds it for this
machine and tags `terva-lampi:dev`; `CONTAINER_ENGINE=podman just image`
builds with Podman. Nothing is pushed. The synthetic image in `e2e/` is a
test fixture and is separate.

## Releases

A release follows the other terva-sh repositories. Pushing a `v*` tag
runs goreleaser on each forge: `.github/workflows/release.yml` publishes
the archives and `checksums.txt` as a GitHub release, and
`.forgejo/workflows/release.yml` uploads the same files to Forgejo with
the `BOT_TOKEN` secret. `install.sh` at the repository root installs
from the GitHub release. Its tests are `TestInstallScript*` in
`internal/cli`.

Tag a commit that is already on both mains, so run `just sync-github
--yes` first, then:

```bash
git tag -a v0.1.0 -m v0.1.0
git push origin v0.1.0
git push github v0.1.0
```

A tag with a hyphen, such as `v0.2.0-rc1`, publishes as a prerelease.
Nothing is linked in with `-X` for a release: `go build` in a checkout
at the tag records the version and commit, and `--version` reads them
back. Both workflows fail when the built binary does not name its tag.
`just release-check` validates `.goreleaser.yaml`, and `just
release-snapshot` builds every archive into `dist/` without a tag.

## Develop on a machine that runs lampi

On a machine that already runs a lake or an agent, use the `dev`
recipes. With no flags, `serve` writes to the agent's state directory
and binds the live lake's port, and `sync` reads the real device token,
machine id, server URL, and allowlist. The recipes put config and state
under `.dev/` in the checkout, put the lake in `.dev/lake`, and bind
`127.0.0.1:18787`. Set `LAMPI_DEV_ADDR` to change the address.

```bash
just dev-serve              # make dev-serve
just dev status             # make dev ARGS=status
just dev sync               # refuses every project until .dev/config allows one
just dev-clean              # make dev-clean
```

The dev allowlist is `.dev/config/terva-lampi/config.json` and starts
empty. `XDG_CONFIG_HOME` also moves the default Cursor IDE and Cursor
CLI roots, so set `harnesses` there to read them.

## Track work

Open work is a git-ticket store in `.tickets/`. A ticket is Markdown
with YAML frontmatter, committed next to the code it describes. Drafts
sit in `.tickets/draft/`. The working set (ready, in progress, blocked,
review) sits in `.tickets/tickets/`. [.tickets/epics.md](../.tickets/epics.md)
lists open epics; `git ticket check --fix` rewrites it.

Install **git-ticket v0.23.0**. Release archives are on the GitHub
releases page. `install.sh` checks the archive against `checksums.txt`
and puts the binary in `~/.local/bin`. Pass `--prefix` for another
writable directory.

```bash
curl -fsSL https://raw.githubusercontent.com/terva-sh/git-ticket/v0.23.0/install.sh | sh
```

With Go, the same tag is:

```bash
go install github.com/terva-sh/git-ticket/cmd/git-ticket@v0.23.0
```

Git runs a program named `git-ticket` as `git ticket`.

```bash
git ticket ready                 # startable, unblocked
git ticket list --status draft   # filed, not yet promoted
git ticket show TKT-…
git ticket check
```

Writes are recorded as `human:sothr` (Drew Short). That actor is the
default in `.tickets/config.yml`, so a write with no `--actor` uses it.
An agent names itself with `--actor`. [AGENTS.md](../AGENTS.md) is the
short workflow an agent session reads, and `git ticket instructions`
prints the long form.
