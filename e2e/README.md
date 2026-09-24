# Synthetic container

Local image for synthetic container smoke. The tag is
`terva-lampi:synthetic` on the local daemon. This target builds and
tags the image. It does not run tests, and it does not push to a
registry. `make ci` and `just ci` do not call it.

```bash
make synthetic-container
```

`just synthetic-container` is the same build. Without either tool:

```bash
docker build -f e2e/Dockerfile -t terva-lampi:synthetic .
```

The entrypoint is `/usr/local/bin/terva-lampi`, which is on `PATH`.
The default command is:

```text
serve --data /lake --addr 127.0.0.1:8787
```

No token file is baked in. `serve` on that loopback address accepts
unauthenticated requests. The bind is loopback inside the container, so
a published host port does not reach it. Override the command at run
time when the driver needs a different one.

The image ships the binary and empty directories. It does not contain
config, tokens, homes, or session fixtures. The driver mounts:

- `/lake` — lake directory (`--data`)
- `/state` — `XDG_STATE_HOME` (set in the image)
- `/config` — `XDG_CONFIG_HOME` (set in the image)
- `/homes/terva`, `/homes/claude`, `/homes/codex`, `/homes/opencode`

Harness environment variables are not set. Point `TERVA_HOME`,
`CLAUDE_CONFIG_DIR`, `CODEX_HOME`, and the OpenCode data directory at
those homes when the driver runs. Soft-link wiring is out of scope.
