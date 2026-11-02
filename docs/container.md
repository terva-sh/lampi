# Run a lake in a container

Back to the [documentation index](README.md).

This guide runs the lake from the `ghcr.io/terva-sh/lampi` image with
Docker Compose, behind Caddy, on a home server or a small VPS. The
systemd setup in [VPS bring-up](vps-bringup.md) is the other supported
way. Both keep the lake in `/var/lib/terva-lampi`, so a lake moves
between them unchanged. Machines that upload still run the agent from the
release archives, not from a container, because the agent reads the
harness directories in your home.

The files are in [`deploy/compose/`](../deploy/compose/): `compose.yaml`,
a `Caddyfile`, and `env.example`.

## What the setup gives you

- The lake runs as user 65532 with a read-only root file system, all
  capabilities dropped, and `no-new-privileges`. Its data is the named
  volume `lake`, mounted at `/var/lib/terva-lampi`.
- Only Caddy publishes ports. It terminates TLS and proxies to the lake
  on the compose network. The lake's own port is not published, so
  nothing reaches it except the proxy.
- The image runs `serve --addr 0.0.0.0:8787 --token-file
  /var/lib/terva-lampi/tokens --behind-proxy`. `--behind-proxy` tells
  `serve` that TLS is in front. It refuses to start without a device
  token, so the lake is never open to requests with no token.
- Docker checks the lake with `serve healthcheck`, and Caddy starts
  once the lake is healthy.
- `stop_grace_period: 60s` gives `serve` time to finish requests (up to
  20s) and queued normalizing (up to 30s) when it stops. Docker's
  default of 10s cuts that off.

## Verify the image

Each release image carries build provenance signed by the GitHub
workflow that built it. To check that an image came from this
repository's release workflow, from a release tag, on a GitHub-hosted
runner:

```bash
gh attestation verify oci://ghcr.io/terva-sh/lampi:0.2.0 \
  --repo terva-sh/lampi \
  --signer-workflow terva-sh/lampi/.github/workflows/release.yml \
  --source-ref refs/tags/v0.2.0 \
  --deny-self-hosted-runners
```

`--repo` alone accepts any workflow in the repository, and `--owner`
accepts any repository in the organization, so keep the signer
workflow. This needs a `gh` recent enough to have the `attestation`
command.

## Before you start

You need Docker with the Compose plugin, a DNS name for the lake, and
`terva-lampi` on the machine you administer from (see the
[README](../README.md#install)).

Pick how Caddy gets its certificate. The choice decides who can reach
the lake:

| Name | Certificate | Needs |
|------|-------------|-------|
| A public name, such as `lake.example.com` | Let's Encrypt, automatically | Ports 80 and 443 reachable from the internet |
| A name under `.home.arpa` or `.internal` | Caddy's own CA, automatically | Each agent machine trusts Caddy's root certificate (see [Trust Caddy's own CA](#trust-caddys-own-ca)) |
| A public name for a lake that is not reachable from the internet | Let's Encrypt over the DNS challenge | A Caddy image built with your DNS provider's module. Caddy's documentation for the DNS challenge covers the build. |

Agents refuse to send a token to a plain `http://` URL that is not
loopback, so the lake needs one of these.

## Set up the directory

1. Copy `deploy/compose/` to the server, for example to
   `/srv/lampi`.
2. Copy `env.example` to `.env` in the same directory.
3. In `.env`, set `LAMPI_VERSION` to a release, such as `0.2.0`, and
   `LAMPI_DOMAIN` to the lake's name.

Pin a release in `LAMPI_VERSION` and change it on purpose. A release
can migrate the catalog, and an older release cannot open a catalog
that a newer one migrated. [Upgrade and roll back](#upgrade-and-roll-back)
covers this.

To run an image you built from a checkout with `just image`, also set
`LAMPI_IMAGE=terva-lampi` and `LAMPI_VERSION=dev`. Podman names the
same image `localhost/terva-lampi`.

Keep `.env` out of git. It holds no secret, but it holds your host name.

## Add the operator token

A lake with no device token refuses to start behind the proxy, and it
refuses to register machines. The first token is yours.

1. On the machine you administer from, make a token:

   ```bash
   terva-lampi login --token-file ./operator.token
   ```

2. On the server, store it in the lake's volume. `login` reads the
   token from stdin and writes it as the lake's user:

   ```bash
   docker compose run --rm -T lampi login \
     --token-file /var/lib/terva-lampi/tokens/operator.token < operator.token
   ```

3. Delete the server's copy of `operator.token`. Keep yours: it is the
   token your own `terva-lampi status` uses.

`serve` hashes the token and rewrites the file with the hash at its
first start, so the volume never keeps the token itself.

## Start the lake

```bash
docker compose up -d
docker compose ps
docker compose logs lampi
```

`docker compose ps` shows `lampi` as `healthy` and `proxy` as running.
The first start logs lines such as:

```text
terva-lampi serve: behind a proxy on 0.0.0.0:8787; TLS terminates in front, and agents use its https:// URL
terva-lampi serve: catalog schema 14, created
terva-lampi serve: made identity lake_...; back up identity.json
terva-lampi serve: 1 device token required
```

Then record the URL agents reach the lake at, and read the key
fingerprint:

```bash
docker compose exec lampi terva-lampi serve identity set-url https://lake.example.com
docker compose exec lampi terva-lampi serve identity
```

Keep the fingerprint where you can read it while you register a
machine. `https://lake.example.com/healthz` now answers
`{"status":"ok"}` through Caddy.

## Register machines

Every `serve` subcommand runs through `docker compose exec` and finds
the lake with no `--data` flag.

1. Mint a code for the machine:

   ```bash
   docker compose exec lampi terva-lampi serve register --name laptop > laptop.code
   ```

   Minting fetches the lake's key list through the public URL, so a
   wrong URL or a proxy problem shows up here and not on the laptop.

2. Move `laptop.code` to the laptop the way you would move a password.
   It works once, for 24 hours.
3. On the laptop, redeem it:

   ```bash
   terva-lampi register --code-file laptop.code --install-service
   ```

`docker compose exec lampi terva-lampi serve devices` lists devices,
and `serve register --list` lists codes. [Registration and
lakes](registration-and-lakes.md) covers the rest.

### Trust Caddy's own CA

With a `.home.arpa` or `.internal` name, Caddy signs the certificate
with its own CA. Each agent machine needs that CA's root certificate,
and so does the lake while it mints a code, because it checks its own
public URL.

Copy the root out of Caddy's volume:

```bash
docker compose cp proxy:/data/caddy/pki/authorities/local/root.crt ./caddy-root.crt
```

Add `caddy-root.crt` to the trust store of each machine that runs an
agent. On Linux, `SSL_CERT_FILE=/path/to/caddy-root.crt` also works for
one command.

For the lake, mount the file into the `lampi` service:

```yaml
    volumes:
      - lake:/var/lib/terva-lampi
      - ./caddy-root.crt:/etc/terva-lampi/caddy-root.crt:ro
```

Run `docker compose up -d`, then pass the file while you mint:

```bash
docker compose exec -e SSL_CERT_FILE=/etc/terva-lampi/caddy-root.crt lampi \
  terva-lampi serve register --name laptop > laptop.code
```

## Reload and restart

- To reload the token file, `identity.json`, and queued normalize jobs
  without a restart, send SIGHUP:

  ```bash
  docker compose kill -s HUP lampi
  ```

- To restart, run `docker compose restart lampi`. The volume keeps the
  lake, its identity, and its devices.

## Run maintenance that needs the lake stopped

`serve compact` and `serve fsck --repair` take the lake lock, so they
refuse while `serve` runs. Stop the service, run the command in a
one-off container on the same volume, and start the service again:

```bash
docker compose stop lampi
docker compose run --rm -T lampi serve compact
docker compose start lampi
```

`serve backup`, `serve devices`, `serve register`, and `serve fsck`
without `--repair` run beside `serve` through `docker compose exec`.

## Back up the lake

`serve backup --out DIR` copies the catalog, the CAS, `identity.json`,
and `audit.jsonl`, and the token file when you pass `--token-file`. It
runs while `serve` runs. The
root file system is read-only, so give the backup a directory of its
own. Add a mount to the `lampi` service:

```yaml
    volumes:
      - lake:/var/lib/terva-lampi
      - ./backups:/backups
      # For encrypted archives: public keys only.
      - ./backup-recipients:/config/backup-recipients:ro
```

Make the directory writable by the lake's user, then run the backup:

```bash
mkdir -p backups && sudo chown 65532:65532 backups && chmod 700 backups
docker compose up -d
docker compose exec lampi terva-lampi serve backup --out /backups/lake \
  --token-file /var/lib/terva-lampi/tokens --prune
```

A second run into the same directory copies only what changed. A run
only adds to it, so a session `serve purge` removed, and the versions
`serve compact` folded away, would stay in the backup for good.
`--prune` removes them once every copy in the run has succeeded: it
deletes from the backup's CAS whatever the backup's own catalog no
longer reaches, and prints what it removed. Copies of the backup made
before the prune, such as older restic snapshots, still hold what it
removed; forget those too after a purge. The
backup is the lake in plaintext, so keep `backups/` on encrypted
storage, and copy it off the host. Back up Caddy's `caddy-data` volume
too, or Caddy requests new certificates after a restore. [Restore
the lake](#restore-the-lake) below has the restore drill.

To copy backups off the host without an encrypted target, write an
[encrypted archive](vps-bringup.md#encrypted-archive) instead. Mount a
recipients file, which holds public keys only, read-only into the
container:

```bash
docker compose exec lampi terva-lampi serve backup \
  --token-file /var/lib/terva-lampi/tokens \
  --recipients-file /config/backup-recipients \
  --archive /backups/lake-$(date -u +%Y%m%dT%H%M%SZ).age
```

The identity that decrypts the archives stays off the lake's host
except during a restore; see [Restore the lake](#restore-the-lake).

### Run it on a schedule

The image has no shell, so a second container in the compose file
cannot loop or run cron. Schedule the backup on the host and have it
`exec` into the running service. Put the commands in a script beside
`compose.yaml`, here `/srv/lampi/backup.sh`:

```bash
#!/bin/sh
set -eu
cd /srv/lampi
docker compose exec -T lampi terva-lampi serve backup --out /backups/lake \
  --token-file /var/lib/terva-lampi/tokens --prune
docker compose exec -T lampi terva-lampi serve backup \
  --token-file /var/lib/terva-lampi/tokens \
  --recipients-file /config/backup-recipients \
  --archive "/backups/lake-$(date -u +%Y%m%dT%H%M%SZ).age"
```

Run it from the host's crontab, or from a systemd timer:

```text
17 3 * * * /srv/lampi/backup.sh >> /var/log/lampi-backup.log 2>&1
```

`-T` is needed because cron gives the command no terminal. Keep the
script in a script file rather than in the crontab line: cron treats
`%` as a newline, which breaks the `date` format.

On Kubernetes, a `CronJob` pod cannot mount a `ReadWriteOnce` volume
that the lake's pod holds on another node. Have the `CronJob` run
`kubectl exec` into the lake's pod instead, with a service account
allowed only to exec into that pod.

### Copy it off the host

Copy only encrypted backups to storage you do not control.

- **Archives** are encrypted, so any tool can copy them, for example
  `rclone copy backups/ remote:lampi --include 'lake-*.age'`.
- **The directory backup** is plaintext. Copy it only with a tool that
  encrypts the target, such as restic or borg, for example
  `restic -r sftp:backup-host:/srv/restic/lampi backup backups/lake`.
  Do not `rsync` or `rclone` the directory to another host as it is.

### Keep and expire backups

The directory backup is one copy that follows the lake, and `--prune`
removes from it what the lake removed. Each archive is a whole copy, so
keep a set and delete the old ones, for example seven daily and four
weekly archives. `serve purge` reaches neither the archives nor
snapshots of the directory made by restic or borg. After a purge,
delete the archives taken before it, and forget and prune the older
snapshots, or the purged sessions stay in them.

### Restore the lake

Restore into a fresh volume, check it, and start the lake on it. Run
the same release that took the backup, or a newer one, which migrates
the catalog when it starts. These steps were run as a drill on
2026-11-02 with v0.10.0 under Podman. Both kinds of restore came back
with the source's lake identity and key, accepted the old device
tokens, and an agent's next sync uploaded nothing.

1. Stop the lake, and point the service at a new volume. In
   `compose.yaml`, change the `lampi` mount to
   `lake-restored:/var/lib/terva-lampi` and add `lake-restored:` under
   `volumes:`. The old volume stays as it was until you remove it.

   ```bash
   docker compose stop lampi
   ```

2. Restore into the new volume. `docker compose run` creates the volume
   owned by the lake's user.

   - **From an archive.** The restore reads the identity, so bring it
     to the lake's host for this one step. Put the archive and the
     identity in `./restore`, mount them read-only, and delete the
     identity from the host when the restore is done:

     ```bash
     docker compose run --rm -T -v ./restore:/restore:ro lampi \
       serve restore --archive /restore/lake-20261102T171915Z.age \
       --identity-file /restore/lampi-backup.key --data /var/lib/terva-lampi
     ```

     It prints the lake it restored, when the archive was taken, and by
     which release.

   - **From the directory backup.** The lake image has no `cp`, so make
     the volume with the lake's image and copy in with a throwaway
     container, as the lake's user. Compose names the volume
     `lampi_lake-restored`:

     ```bash
     docker compose run --rm -T lampi --version
     docker run --rm --user 65532:65532 -v lampi_lake-restored:/lake \
       -v ./backups/lake:/backup:ro docker.io/library/alpine:3 cp -a /backup/. /lake/
     ```

     Copy into a volume made this way, not with `docker volume
     import` or as root. Those leave the volume's top directory owned by
     root, and `serve` then fails with `lakelock: ... permission denied`.

3. Check the store, then start the lake:

   ```bash
   docker compose run --rm -T lampi serve fsck
   docker compose up -d lampi
   docker compose logs lampi | grep identity
   ```

   The log names the same `lake_...` identity as before. A different
   one means the restore made a new lake, and agents will refuse it.

4. From a machine that syncs to the lake, run `terva-lampi sync` and
   `terva-lampi status`. The sync uploads nothing it had already sent,
   and `lake_normalization` reaches `pending=0` as the lake rebuilds
   its derived files.

Restore Caddy's `caddy-data` volume the same way, or Caddy asks for new
certificates.

## Upgrade and roll back

To upgrade, read the release notes for a catalog migration, then change
`LAMPI_VERSION` in `.env` and recreate the service:

```bash
docker compose pull lampi
docker compose run --rm -T lampi serve migrate --check
docker compose up -d lampi
docker compose logs lampi | grep catalog
```

`serve migrate --check` in the new image reads the current catalog
without changing it and reports how many migrations are pending. When
`serve` starts and migrations are pending, it first copies the catalog
to `migration-backups/` in the volume, keeps the newest three copies,
and logs the path and each step. If the copy fails, `serve` does not
migrate and does not start.

An older release refuses a catalog that a newer one migrated. The lake
image has no shell, so the rollback uses a throwaway Alpine container on
the same volume. Compose names the volume `lampi_lake`.

1. Stop the service:

   ```bash
   docker compose stop lampi
   ```

2. List the copies. The newest name sorts last, and it ends in
   `-v<old schema>.db`:

   ```bash
   docker run --rm -v lampi_lake:/lake docker.io/library/alpine:3 ls /lake/migration-backups
   ```

3. Put the copy in place of `catalog.db`. Remove `catalog.db-wal` and
   `catalog.db-shm` in the same step. They belong to the newer catalog,
   and SQLite would apply them to the restored file:

   ```bash
   docker run --rm -v lampi_lake:/lake docker.io/library/alpine:3 sh -c '
     cp /lake/migration-backups/COPY.db /lake/catalog.db &&
     rm -f /lake/catalog.db-wal /lake/catalog.db-shm &&
     chown 65532:65532 /lake/catalog.db && chmod 600 /lake/catalog.db'
   ```

4. Set `LAMPI_VERSION` in `.env` back to the older release, and start
   it:

   ```bash
   docker compose up -d lampi
   ```

Manifests accepted since the upgrade are not in the restored catalog,
so roll back soon after an upgrade, not days later.

A release that stores blobs compressed writes each new object as a
`.zst` file, and a release from before it cannot read one. After such an
upgrade, a rollback reads every blob stored before the upgrade and none
stored since. So take a backup before that upgrade, and restore it to
roll back.

Do not let a tool such as Watchtower pull a new release on its own. An
update tool that opens a pull request for the version bump, such as
Renovate, leaves you the step of reading the release notes.

## Add the dashboard or metrics

Both are flags on `serve`, so add a `command:` to the `lampi` service
that repeats the image's flags and adds yours.

- The OIDC dashboard reads a web configuration file. Mount it and its
  client secret file read-only, and pass `--web-config`.
  [Serving the dashboard](web-dashboard.md) covers the file:

  ```yaml
      command: ["serve", "--addr", "0.0.0.0:8787",
                "--token-file", "/var/lib/terva-lampi/tokens", "--behind-proxy",
                "--web-config", "/etc/terva-lampi/web.json"]
      volumes:
        - lake:/var/lib/terva-lampi
        - ./web.json:/etc/terva-lampi/web.json:ro
        - ./client-secret:/etc/terva-lampi/client-secret:ro
  ```

- Prometheus metrics have no authentication. `serve` refuses a
  non-loopback `--metrics-addr` unless you also pass
  `--metrics-public`, and inside a container the scraper can reach only
  a non-loopback address. Pass both only when the scraper is on the
  compose network, and never publish the metrics port:

  ```yaml
      command: ["serve", "--addr", "0.0.0.0:8787",
                "--token-file", "/var/lib/terva-lampi/tokens", "--behind-proxy",
                "--metrics-addr", "0.0.0.0:9187", "--metrics-public"]
  ```

Caddy does not route `/metrics`. The metrics listener is a separate
port, reachable only on the compose network.

## Read the logs

`serve` writes one line per request to stderr: method, path, status,
sizes, duration, remote address, and `X-Forwarded-For` as Caddy sent it.
A 200 `/healthz` is not logged, so the healthcheck does not fill the
log. `docker compose logs lampi` shows them.

## Run it with Podman

The compose file works with `podman-compose`. Three things differ:

- Rootless Podman cannot bind ports below 1024. Publish Caddy on other
  ports and forward 80 and 443 to them, or run Podman as root.
- Named volumes need nothing extra. A bind mount that the lake writes,
  such as `./backups`, needs `--userns keep-id:uid=65532,gid=65532` on
  the container, or a `chown` to the user that 65532 maps to. Use the
  same `--userns` for every container on the volume, including the
  `login` that writes the operator token. Otherwise 65532 maps to a
  different host user in each, and `serve` cannot read the token
  directory.
- `podman build` drops `HEALTHCHECK` in its default OCI format. The
  published image keeps it. `CONTAINER_ENGINE=podman just image` builds
  in Docker format for this reason.
