# Getting started

In this tutorial we build `terva-lampi`, run a lake on this machine,
allow one project, and upload its agent transcripts. At the end you
have a lake with that project's sessions in it and the agent watching
for more. Back to the
[documentation index](README.md).

You need Go 1.27 and a checkout of this repository. Nothing here needs
root, and nothing leaves the machine: the lake listens on loopback.

If this machine already runs a lake or an agent, stop here and use the
[development recipes](development.md#develop-on-a-machine-that-runs-lampi)
instead. With no flags, the commands below read and write the same
state as the live ones.

## Build the binary

```bash
make build
```

We now have `bin/terva-lampi`. `just build` builds the same binary.

## Start a lake

```bash
./bin/terva-lampi serve
```

`serve` prints where it keeps its data and that it has no device token:

```text
terva-lampi serve: listening on 127.0.0.1:8787
terva-lampi serve: data /home/you/.local/state/terva-lampi
terva-lampi serve: made identity lake_…; back up identity.json
terva-lampi serve: no device token configured; accepting unauthenticated requests
```

Without a token, `serve` accepts requests only on a loopback address,
so the lake is private to this machine. Leave it running and open a
second terminal for the rest of the steps.

```bash
curl -sS http://127.0.0.1:8787/healthz
```

```text
{"status":"ok"}
```

## See what the agent would upload

```bash
./bin/terva-lampi agent discover
```

This lists every transcript the agent can find, one per line, with the
harness first:

```text
terva   sessions/a1b2c3d4e5f60708/20260922-161000-abcd1234.jsonl
claude  projects/-home-you-src-foo/5f0c….jsonl
```

If a harness you use is missing, check where it is read in
[Harnesses](harnesses.md).

## Try a sync, and see it refused

```bash
./bin/terva-lampi sync
```

Nothing uploads yet. Every project is refused until you allow it, and
`sync` names each refused session with the values an allow rule can
match:

```text
checked 0, missing 0, uploaded 0, manifests 0, refused 1, quarantined 0, unchanged 0
terva-lampi: upload: refused off-box raw:
sessions/…/20260922-161000-abcd1234.jsonl: not allowlisted for off-box raw (cwd "/home/you/src/foo", cwd_hash "03ce1a5e50756ee4", git_remote "")
```

## Allow one project

Pick one project whose transcripts you are happy to copy to the lake.
Create `~/.config/terva-lampi/config.json` with one allow rule for it:

```json
{
  "projects": {
    "allow": [{"cwd_prefix": "/home/you/src/foo"}]
  }
}
```

Replace the path with the `cwd` that `sync` printed. Sync again:

```bash
./bin/terva-lampi sync
```

```text
checked 1, missing 1, uploaded 1, manifests 1, refused 0, quarantined 0, unchanged 0
```

The lake now holds that transcript. Run `sync` once more and nothing
uploads, because the lake already has those bytes:

```text
checked 0, missing 0, uploaded 0, manifests 0, refused 0, quarantined 0, unchanged 1
```

Before you allow more projects, read
[Allowlist and redaction](allowlist-and-redaction.md). It explains deny
rules, git remote rules, and the secret scan that holds back a file
with a token in it.

## Check the status

```bash
./bin/terva-lampi status
```

`status` shows each harness, the last sync, and what the lake holds:

```text
harness terva enabled=true root=/home/you/.local/state/terva source=default
…
lake: default
outbox: 0
last_attempt: 2026-09-27T14:48:48Z ok
last_error: none
server: http://127.0.0.1:8787 source=default
health: ok
catalog_sessions: 1
catalog_artifacts: 1
catalog_machines: 1
```

## Keep it running

```bash
./bin/terva-lampi agent
```

The agent pushes once, then watches the harness directories and pushes
again a few seconds after a transcript grows. Stop it with Ctrl-C. To
run it as a service, use the systemd or launchd examples in
[deploy/README.md](../deploy/README.md).

## Where to go next

- Add a laptop or a second machine to this lake:
  [Registration and lakes](registration-and-lakes.md).
- Host the lake on a server with TLS and backups:
  [VPS bring-up](vps-bringup.md).
- Browse sessions in a browser: [Serving the dashboard](web-dashboard.md).
- Export sessions as JSONL or a ShareGPT dataset:
  [Command reference](cli.md#export).
