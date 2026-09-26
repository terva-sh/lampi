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

## Browser dashboard smoke

The browser fixture is separate from the production binary. It creates a temporary
lake with 123 synthetic sessions, a fake HTTPS IdP and ephemeral loopback ports,
and removes its lake on shutdown. No agent config or production token is read.

Use Go 1.27 and Node 22 or newer. Install Playwright outside the repository; it is
only a test dependency, and the dashboard itself has no Node build. On a machine
with mise, prefix the final command with `mise exec --` so its child Go build sees
the managed toolchain.

```sh
browser_tools_dir=$(mktemp -d)
npm install --prefix "$browser_tools_dir" --no-audit --no-fund playwright@1.58.2
"$browser_tools_dir/node_modules/.bin/playwright" install chromium
node e2e/web-smoke.mjs "$browser_tools_dir/node_modules/playwright/index.mjs"
```

Chromium must have its normal OS runtime dependencies installed. The script builds
`internal/web/smoketest` in a temporary directory, drives a real OIDC code/PKCE
flow with the fake IdP, and prints a JSON result plus a temporary evidence directory
containing desktop/mobile screenshots. `ignoreHTTPSErrors` applies only to this
test browser and its synthetic self-signed IdP. Production issuer validation is
unchanged. The fixture shuts down even when a browser assertion fails.

The smoke covers viewer and denied-group login, pagination, filters, artifact and
provenance views, conflicts, polling visibility/error/recovery/expiry, desktop and
390px mobile layouts, keyboard focus, logout, no-JavaScript filtering and an empty
lake. Unit/integration gates additionally verify state/nonce/replay, key rotation,
algorithm refusal, idle/hard expiry, CSRF, device/browser separation and concurrent
ingest. `go test ./internal/catalog -run TestDashboard20K -count=1 -v` records query
plans, response sizes and timings for 20,000 sessions, including concurrent writes.

For manual inspection, run `go run ./internal/web/smoketest` and open the printed
`url` in a test browser that trusts the fixture's HTTPS IdP. Stop with Ctrl-C. Use
`--deny` or `--empty` to inspect those states. Never use this fixture as a service.

## Isolated go-live drills

The `golive` build tag enables operational drills with explicit temporary harness
roots, config, state and lake directories. They do not read live agent data.

```sh
go test -tags golive ./internal/cli -run '^TestGoLive(Canaries|Restore|ProcessCrash)$' -count=1 -timeout=5m -v
go test -tags golive ./internal/cli -run '^TestGoLive20KSync$' -count=1 -timeout=20m -v
```

The first command verifies canary exclusion from CAS/catalog/normalized events,
a live backup restored into a new directory, and SIGKILL recovery of a real
server subprocess. The crash test requires Unix and does not simulate loss of
the host page cache or VM power. The scale drill seeds 5,000 sessions per harness
(Terva, Claude, Codex, OpenCode), records both CLI sync times, and verifies the
unchanged second pass sends no manifest POSTs or blob PUTs.

The slow-uplink drill requires an existing Traefik file provider and a trusted
HTTPS origin routed to it. Set the following variables to operator-supplied
values, outside source control:

```sh
LAMPI_GOLIVE_ORIGIN="$https_origin" \
LAMPI_GOLIVE_TRAEFIK_DIR="$dynamic_config_dir" \
  go test -tags golive ./internal/cli -run '^TestGoLiveSlowTLS$' -count=1 -timeout=10m -v
```

This opt-in drill creates one uniquely named temporary route to a synthetic
loopback lake protected by an ephemeral in-memory device bearer. It preserves
TLS certificate verification and existing routes. It uploads a valid 32 MiB
session through the actual proxy with request bodies paced at 250,000 bytes/s
(2 Mbit/s), using the uploader's default pieces, then checks normalization.
It removes its route when finished. A forced termination of the test process
may leave its `lampi-golive-*.tmp.yml` file in the provider directory; inspect
and remove only that drill's file before continuing. The script does not change
host networking or apply a system-wide traffic shaper.
