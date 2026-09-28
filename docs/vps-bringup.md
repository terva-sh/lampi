# VPS lake bring-up

This is the operator checklist for the Phase 0 lake. Read it when you
host a lake on a server. For a lake on one machine, start with
[Getting started](getting-started.md). Back to the
[documentation index](README.md).

[policy.md](policy.md) is the decision. This file does not provision
a host, open a disk, or store a token. Copy the examples and edit
them on the machine.

The lake is `terva-lampi serve` on a small VPS with local disk. TLS
sits in front of that process. The process binds `127.0.0.1:8787`.
Agents set `server` in `config.json`, or `LAMPI_SERVER`, to the host's
HTTPS URL. There is no TTL.
Encryption at rest is the provider's volume encryption and/or LUKS
on the data disk. There is no application-level age wrapping.

`lake.example` below is a placeholder. It is not a host this tree
runs. Replace it on the machine, and do not commit the result.

## Disk

Choose or create a small VPS whose data disk is encrypted. The
provider's volume encryption is enough. LUKS on that disk is the
other accepted choice. Use one or both. CAS objects and `catalog.db`
sit on that volume.

This tree does not open the disk. On the host, a volume the provider
already encrypted is a mount you put the lake directory on. A LUKS
disk you are formatting looks like the commands below. `EXAMPLE` is
not a device name. Put the real disk id in its place. `luksFormat`
erases that disk.

```bash
sudo cryptsetup luksFormat /dev/disk/by-id/EXAMPLE
sudo cryptsetup open /dev/disk/by-id/EXAMPLE terva-lampi-data
sudo mkfs.ext4 /dev/mapper/terva-lampi-data
sudo mkdir -p /var/lib/terva-lampi
sudo mount /dev/mapper/terva-lampi-data /var/lib/terva-lampi
```

Add the mapper and the mount to crypttab and fstab yourself, with
the options that image uses. The example serve unit has a commented
`Requires=` for that mount. Uncomment it when the data directory is
a separate filesystem, and use the mount unit name systemd derives
from the path.

## Binary

Install from a checkout until a release is tagged. The Go line is
1.27, the same as the module.

```bash
go build -o bin/terva-lampi ./cmd/terva-lampi
sudo install -m 0755 bin/terva-lampi /usr/local/bin/terva-lampi
```

The example unit starts `/usr/local/bin/terva-lampi`. Change
`ExecStart` if the binary lives somewhere else. `make build` does
not install it.

## Data directory

`--data` is the lake directory. Point it at the encrypted mount.
The example path is `/var/lib/terva-lampi`. `serve` creates the
contents when it starts. Those directories are mode 0700.

```text
/var/lib/terva-lampi/cas/sha256/…     content-addressed blobs
/var/lib/terva-lampi/cas/logical/…    chunk lists for files over 32 MiB,
                                      and versions kept as a prefix of the
                                      file they grew into
/var/lib/terva-lampi/cas/partial/…    resumable uploads in flight
/var/lib/terva-lampi/catalog.db       SQLite catalog, plus WAL sidecars
/var/lib/terva-lampi/normalized/      one JSONL file per session
/var/lib/terva-lampi/parquet/         date=…/harness=… partitions
/var/lib/terva-lampi/identity.json    lake id and private signing keys
/var/lib/terva-lampi/tokens           host copy of device tokens
```

`serve` makes `identity.json` at mode 0600 on its first start and prints
the lake id. Agents pin its key, so back it up with the catalog. A
catalog that has recorded a lake id refuses to start without its
`identity.json`. `terva-lampi serve identity` prints the lake id and each
key's fingerprint.

The default without `--data` is the XDG state dir `terva-lampi/`
(`$XDG_STATE_HOME/terva-lampi`, or `~/.local/state/terva-lampi`).
On the VPS, pass `--data`. Otherwise the bytes land in that default
and may miss the encrypted volume.

The example unit runs as the system user `terva-lampi`. `useradd`
creates the matching group. Create both, and the directory, before
enabling the unit.

```bash
sudo useradd --system --user-group --no-create-home --shell /usr/sbin/nologin terva-lampi
sudo install -d -o terva-lampi -g terva-lampi -m 0700 /var/lib/terva-lampi
```

## Devices

Each machine that uploads is a device with its own token. The lake
stores only a hash of each token. There are two ways to add one.

### Registration (the usual way)

`serve` needs a token file even when every device registers, because a
lake with none accepts requests without a token and refuses to
register devices. One operator token in a directory is enough:

```bash
sudo -u terva-lampi install -d -m 0700 /var/lib/terva-lampi/tokens
terva-lampi login --token-file ./operator.token   # on your own machine
```

Copy `operator.token` into that directory as `operator.token`, and start
`serve` with `--token-file /var/lib/terva-lampi/tokens`. Then, once
TLS is in front (below), record the URL agents reach the lake at:

```bash
sudo -u terva-lampi terva-lampi serve identity set-url https://lake.example --data /var/lib/terva-lampi
sudo -u terva-lampi terva-lampi serve identity --data /var/lib/terva-lampi
```

The second command prints the lake id, the public URL and the key
fingerprint. Keep the fingerprint where you can read it when a machine
registers. For each machine, mint a code:

```bash
sudo -u terva-lampi terva-lampi serve register --name laptop --data /var/lib/terva-lampi > laptop.code
```

Minting fetches the key list through the public URL and refuses when
it does not reach this lake, so a wrong URL or a proxy that drops
`/.well-known/terva-lampi/` shows up here and not on the laptop. The
code works once, for 24 hours (`--expires`), and is a secret: move it
like a password. `--profile` chooses the base configuration its agent
gets (see Profiles below). `serve register --list` shows codes and
their state; `--revoke laptop` stops a pending one. The machine side is
in [Check, then point the agents](#check-then-point-the-agents).

### A token file by hand (the fallback)

On each machine that will upload, mint a token. `terva-lampi login`
writes a 256-bit token to `~/.config/terva-lampi/token` at mode 0600
and does not print it. `--token-file` chooses another path. The
token is not a command argument.

```bash
terva-lampi login
```

Copy that file to the VPS. The copy is yours to make. This tree does
not log in to a host. The host path is a different file from the
client's. Keep the client's file. It stays the secret.

`serve --token-file` hashes each token with SHA-256 and rewrites the
host copy to lines of `sha256:<hex>`, mode 0600. One file holds one
token per line, or a directory holds one `<name>.token` file per
device. A directory file with another name is not loaded; `serve`
names it on stderr. Before this rule every file there was loaded, so
rename device files to `.token` when you upgrade. A token is 64
lowercase hex characters, as `login` writes. A line starting with `#`
is a comment, such as the device's name, and stays through the
rewrite. Any other line stops `serve` with the file and line number.
There is no TTL. A machine added this way has no pinned lake key and
gets no base configuration; register it instead when you can.

To add a device, add its token to the file and send `serve` SIGHUP
(`sudo systemctl kill -s HUP terva-lampi-serve`). Requests in flight
keep going. A file that does not load leaves the old tokens in place
and says why on stderr.

Each token is a named device in the catalog: the name given to
`serve register`, or the `<name>.token` file in a directory, or the
`#` comment line just above the token in a file, or `token-N`. A
registered device is bound to its machine when it registers. A
token-file device binds to the first
`machine_id` it uploads a manifest under. A manifest from another
machine, or a `machine_id` another device holds, is refused with 403.

```bash
sudo -u terva-lampi terva-lampi serve devices --data /var/lib/terva-lampi
sudo -u terva-lampi terva-lampi serve devices revoke laptop --data /var/lib/terva-lampi
sudo -u terva-lampi terva-lampi serve devices unbind desktop --data /var/lib/terva-lampi
```

`revoke` takes effect on the lake's next request, with no signal, and
is final. For a token-file device, remove the token from the file as
well. A token removed
from the file shows as `detached`. `unbind` lets a reinstalled machine
with a new machine id bind again. Device changes are appended to
`audit.jsonl` in the data directory, which `serve backup` copies.

`serve` replaces the file in that directory, so the service user has
to be able to write there. The example path is on the encrypted
volume, next to the catalog. The example unit makes the file system
read-only except `/var/lib/terva-lampi`. A token file somewhere else
needs its directory added to `ReadWritePaths=`. Otherwise `serve`
stops at start with `read-only file system` when it rewrites the
file.

```bash
sudo install -m 0600 -o terva-lampi -g terva-lampi ./token /var/lib/terva-lampi/tokens
```

The agent, `sync`, and `status` read `--token-file`, then
`LAMPI_TOKEN_FILE`, then `config.json`, then
`~/.config/terva-lampi/token`. `serve` reads only
`--token-file`. It does not read `LAMPI_TOKEN_FILE`.

### Profiles

A lake can give its registered agents a base configuration. Profiles
live in the lake's catalog, and every save keeps a revision. A lake
with no `default` profile serves an empty one. `serve` does not read
`profiles.json` or `--profiles`. `deploy/profiles.json.example` shows
the shape of a profile. As written, it allows only a placeholder
remote, so an agent with no allow rules of its own uploads nothing.
A profile may set harnesses on or off, the debounce, and `projects.allow` and
`projects.deny` for uploads to this lake. It cannot set a harness root
or `redaction.upload_hits`; a profile that tries is refused. A
device gets the `default` profile unless its code named one or
`serve devices set-profile laptop NAME` changes it. The machine's own
`config.json` wins over every field. A saved profile applies at each
agent's next fetch.

## Serve on loopback

The default bind is `127.0.0.1:8787`. Keep it. A non-loopback
`--addr` without `--token-file` is an error. With a token file,
still do not bind a public address; `serve` warns on stderr when it
is. The reachable listener is the TLS endpoint in front of this
process.

From the checkout:

```bash
sudo install -m 0644 deploy/systemd/terva-lampi-serve.service /etc/systemd/system/terva-lampi-serve.service
sudo install -d -m 0755 /etc/terva-lampi
sudo install -m 0600 deploy/systemd/serve.env.example /etc/terva-lampi/serve.env
sudo systemctl daemon-reload
sudo systemctl enable --now terva-lampi-serve.service
```

`serve.env` is optional. The unit already carries the loopback
address, `/var/lib/terva-lampi`, and `/var/lib/terva-lampi/tokens`.
Uncomment a line in the env file to override one of them. systemd
substitutes `LAMPI_SERVE_ADDR`, `LAMPI_SERVE_DATA`, and
`LAMPI_SERVE_TOKEN_FILE` into `--addr`, `--data`, and `--token-file`.
The process does not read those names.

The unit sandboxes the process. It has no capabilities and cannot
gain privileges. It cannot see `/home`, and it gets a private `/tmp`
and a minimal `/dev`. The file system is read-only except
`ReadWritePaths=`, which is `/var/lib/terva-lampi`. If you move
`--data` or `--token-file`, change that line to match. `UMask=0077`
keeps what `serve` creates private to the service user.
`TimeoutStopSec=120` gives `serve` time to finish queued
normalization when it stops.

`serve` writes one line per request to stderr, so
`journalctl -u terva-lampi-serve` shows them. A 200 `/healthz` is not
logged. `X-Forwarded-For` is logged as the proxy sent it.
`Authorization` is not logged. `systemctl stop` waits up to 20s for
requests in flight and 30s for the normalize queue. Jobs the drain
did not reach run at the next start.

Nothing in `deploy/` is installed by `make build`.

## TLS in front

Put a TLS terminator on the public interface and proxy to
`127.0.0.1:8787`. Do not publish port 8787. Plain HTTP for the lake
stays on loopback. A public port 80 that only answers a certificate
challenge is not the lake. Do not proxy `serve` on that port.

A blob upload is up to 32 MiB in one request. The proxy has to pass
a body that size, and give it time on a slow uplink.

Caddy, copy and edit. `reverse_proxy` streams the request body and
has no default body size limit, so this needs nothing more.

```text
lake.example {
	reverse_proxy 127.0.0.1:8787
}
```

Two routes answer without a token: `POST /v1/register` and
`GET /.well-known/terva-lampi/keys`. `serve` limits them together to a
burst of 20 and 5 a second for the whole lake. Behind a proxy every
caller has the proxy's address, so limit them per client address at
the proxy as well. Core Caddy has no rate limiter; with the
`caddy-ratelimit` module built in, add:

```text
lake.example {
	@open path /v1/register /.well-known/terva-lampi/*
	rate_limit @open {
		zone open {
			key    {remote_host}
			events 10
			window 1m
		}
	}
	reverse_proxy 127.0.0.1:8787
}
```

nginx is the same shape, with more lines. Its default body limit is
1 MiB, so a larger blob gets `413`. It also buffers the whole body
before it proxies, and times out after 60 seconds. The snippet below
raises the limit to 40 MiB, streams the body, and allows 300
seconds. `serve` has no fixed read timeout: each request's deadline
is a floor plus the body at 64 KiB/s. The
certificate paths are placeholders. They belong on the host, not in
git.

```text
# In the http block: ten a minute per client address.
limit_req_zone $binary_remote_addr zone=lampi_open:1m rate=10r/m;

server {
	listen 443 ssl;
	server_name lake.example;

	ssl_certificate     /etc/ssl/certs/lake.example.crt;
	ssl_certificate_key /etc/ssl/private/lake.example.key;

	client_max_body_size 40m;

	location ~ ^/(v1/register|\.well-known/terva-lampi/) {
		limit_req zone=lampi_open burst=10 nodelay;
		proxy_pass http://127.0.0.1:8787;
		proxy_set_header Host $host;
	}

	location / {
		proxy_pass http://127.0.0.1:8787;
		proxy_set_header Host $host;
		proxy_http_version 1.1;
		proxy_request_buffering off;
		proxy_read_timeout 300s;
		proxy_send_timeout 300s;
	}
}
```

## Check, then point the agents

On the VPS, the process probe is loopback. `/healthz` returns
`{"status":"ok"}` and no catalog data. It does not need a token.

```bash
curl -fsS http://127.0.0.1:8787/healthz
```

The same path through TLS is what the agents depend on. Replace the
placeholder before you run it.

```bash
curl -fsS https://lake.example/healthz
```

On each uploading machine, copy the code over and register. On a fresh
machine, `--install-service` also writes and starts the agent's user
unit (systemd) or launchd agent:

```bash
terva-lampi register --code-file laptop.code --install-service
shred -u laptop.code
```

`register` checks the code's signature and expiry, that the URL is
https, and that the key list there holds the code's key. It then shows
the URL, lake id and fingerprint. Compare the fingerprint with `serve
identity` on the VPS and confirm; with no terminal, pass
`--fingerprint SHA256:…`. It writes the token to
`~/.config/terva-lampi/tokens/<name>.token` without printing it, the
lake with its pinned key into `config.json`, and the lake's profile. A
running agent picks the lake up at once on Linux and macOS. On Windows,
restart the agent to add a lake. `terva-lampi lakes` lists the lakes a
machine reports to.

A machine using a hand-copied token instead sets `LAMPI_SERVER` to the
HTTPS URL, or `server` in `config.json`. The agent unit reads
`~/.config/terva-lampi/agent.env` (mode 0600) and sets neither value
itself. The copy of that file in git keeps the loopback URL. Put the
real URL only in the file on the machine. `terva-lampi status` prints
the URL with `source=env` or `source=config`.

```text
LAMPI_SERVER=https://lake.example
LAMPI_TOKEN_FILE=/home/you/.config/terva-lampi/token
```

Restart the agent after either value changes. `GET /v1` requires
`Authorization: Bearer` and the device token. `/healthz`, the key list
and `POST /v1/register` do not. The agent, `sync`, `status`, and
`conflicts` refuse to send the token to an `http://` URL whose host is
not `localhost`, 127.0.0.0/8, or `::1`. A lake that answers 401 or 403
is logged once, naming the token file, and the agent retries every
five minutes until it is accepted. A registered machine also checks
the lake's key on every connection and pushes nothing to a lake that
does not prove it.

Laptop, desktop, and the remote/cloud box each run `terva-lampi
agent`. The names stay at the role. [policy.md](policy.md) lists
them.

## Backup

`terva-lampi serve backup --out DIR` copies the catalog, then
`cas/sha256` and `cas/logical`, then `identity.json`, then the token
file with `--token-file`. Run it as the service user. It runs while `serve`
runs. The catalog copy is `VACUUM INTO`, one consistent snapshot, and
the CAS is copied after it. A second run into the same directory
copies only new objects. Keep DIR on encrypted storage.
`terva-lampi serve fsck` re-hashes every object and exits non-zero
when one is bad.

Without the command, the order is the same: the catalog first, then
`cas/sha256`, then `cas/logical`, then `identity.json`, then the token
file.

Do not `cp` `catalog.db` while `serve` runs. The catalog is in WAL
mode, and a plain copy can miss or tear the WAL. Use the SQLite
online backup. It needs the `sqlite3` command, which this tree does
not install. Run it as the service user, so a sidecar it creates
stays theirs.

```bash
sudo install -d -o terva-lampi -g terva-lampi -m 0700 /var/backups/terva-lampi
sudo -u terva-lampi sqlite3 /var/lib/terva-lampi/catalog.db ".backup '/var/backups/terva-lampi/catalog.db'"
```

Then copy the CAS. While `serve` runs, a transcript that grows has its
previous version's object, or for a file past 32 MiB its last chunk,
replaced by a prefix record in `cas/logical`, written after the grown object it points at. Copy
`cas/sha256`, then `cas/logical`, then `cas/sha256` again, so each
record copied finds the object it reads from. `serve backup` does
this and follows every record in the copy. `serve purge`,
`serve compact` and `serve fsck --repair` remove objects, and all
three refuse while `serve` holds the lake. Leave out the `.put-*`
and `.logical-*` temp files. The example uses `rsync`. Any copy that
keeps the tree works. Run `serve fsck --data` on the copy to check it.

```bash
sudo rsync -a --exclude '.put-*' /var/lib/terva-lampi/cas/sha256 /var/backups/terva-lampi/cas/
sudo rsync -a --exclude '.logical-*' /var/lib/terva-lampi/cas/logical /var/backups/terva-lampi/cas/
sudo rsync -a --exclude '.put-*' /var/lib/terva-lampi/cas/sha256 /var/backups/terva-lampi/cas/
sudo install -m 0600 -o terva-lampi -g terva-lampi /var/lib/terva-lampi/tokens /var/backups/terva-lampi/tokens
```

`cas/logical` is not optional. A file over 32 MiB is stored as
chunks, and its entry there is how the lake reads it back. Without
it, that file cannot be read and its session does not project.

The token file holds only `sha256:` lines once `serve` has run.
Without it, every agent gets `401` after a restore until you copy
each device token to the host again.

Leave out `cas/partial/`. It holds uploads in flight, and the agent
sends those again. `normalized/` and `parquet/` are derived from the
catalog and the CAS. `terva-lampi export` rebuilds a session whose
JSONL is missing, and writes that session's parquet with it. Copy
them too if a restore should need no rebuild.

### Restore drill

The isolated `TestGoLiveRestore` drill runs a real `serve` process, syncs
synthetic Terva, Claude, Codex and OpenCode sessions, and takes a live backup.
It copies that backup into a fresh directory, checks the CAS without repair,
rebuilds the missing derived files while the restored lake is stopped, then
starts another `serve` process on an ephemeral loopback port. Both report four
sessions, four artifacts and one machine; event content and raw references match.

Reproduce the drill without reading a live lake or agent configuration:

```bash
go test -tags golive ./internal/cli -run '^TestGoLiveRestore$' -count=1 -v
```

The command sequence exercised by the drill is below. The variables represent
fresh, isolated directories and separate loopback listeners created by the test;
do not point the restored process at the source directory.

```bash
terva-lampi serve backup --data "$source_dir" --out "$backup_dir"
# Copy the complete backup to a fresh restore_dir, preserving private modes.
terva-lampi serve fsck --data "$restore_dir"
terva-lampi export --data "$restore_dir" --out "$rebuilt_events"
terva-lampi serve --data "$restore_dir" --addr "$restore_addr"
terva-lampi status --server "$source_url"
terva-lampi status --server "$restore_url"
terva-lampi export --data "$restore_dir" --out "$restored_events"
```

Export before starting the restored listener is necessary: export beside a live
server does not rebuild missing derived files. Reprojection generates new
`event_id` and `ingested_at` values, and a new `recorded_at` where the source
had no timestamp and projection time was used as a fallback. The drill checks
all other fields exactly, including recorded timestamps supplied by the harness.
Export remains byte-identical before and after starting the restored listener.
Copy the derived files as well if preserving generated event IDs is required.
The synthetic drill does not validate a production token backup or encrypted
backup storage; verify those and protect external web/OIDC configuration during
the actual deployment backup.

The backup holds the lake in plaintext. Keep it on encrypted
storage, the same as the data disk.

### Restore drill

Run this once before the lake matters, and again after the layout
changes. On an uploading machine, note the counts first.

```bash
terva-lampi status
```

Keep the `catalog_sessions`, `catalog_artifacts`, and
`catalog_machines` lines. On the VPS, stop `serve`, then take the
backup above. Nothing changes the lake while `serve` is stopped, so
the backup holds those counts. An upload that lands between `status`
and the stop raises them. If the counts differ at the end, run the
drill again.

```bash
sudo systemctl stop terva-lampi-serve.service
```

Move the old data directory aside and restore into a fresh one. If
`/var/lib/terva-lampi` is a mount point, move its contents aside
instead of the directory.

```bash
sudo mv /var/lib/terva-lampi /var/lib/terva-lampi.old
sudo install -d -o terva-lampi -g terva-lampi -m 0700 /var/lib/terva-lampi
sudo cp -a /var/backups/terva-lampi/. /var/lib/terva-lampi/
sudo chown -R terva-lampi:terva-lampi /var/lib/terva-lampi
```

If you left out `normalized/` and `parquet/`, rebuild them before
`serve` starts.

```bash
sudo -u terva-lampi terva-lampi export --data /var/lib/terva-lampi --out /dev/null
```

Start `serve` and run `terva-lampi status` again on the uploading
machine. The three `catalog_` counts match the ones you kept. Delete
`/var/lib/terva-lampi.old` only after they do.

```bash
sudo systemctl start terva-lampi-serve.service
```

## Compact

Ingest keeps a transcript's older versions as prefix records of the
newest, and a transcript past 32 MiB its older last chunks, but the tails a client sent stay stored, and a lake written
before prefix records holds each version whole. `serve compact` folds
every older version that is a prefix of its file's newest into a
record, points existing records at the newest, and removes the
objects nothing reads from. Each fold is hash-checked against the
newest's bytes first.

See what it would do first. The dry run writes nothing and can run
beside `serve`.

```bash
sudo -u terva-lampi terva-lampi serve compact --data /var/lib/terva-lampi --dry-run
```

Then stop `serve`, compact, check the store, and start it again.
Take a backup first if the disk has room. An older `terva-lampi`
cannot read prefix records, so after the first compact a rollback
needs that backup.

```bash
sudo systemctl stop terva-lampi-serve.service
sudo -u terva-lampi terva-lampi serve compact --data /var/lib/terva-lampi
sudo -u terva-lampi terva-lampi serve fsck --data /var/lib/terva-lampi
sudo systemctl start terva-lampi-serve.service
```

An unreferenced object written in the last hour is kept, since it
may be a blob put for a manifest not yet posted. `--min-age` changes
the window. Compact is safe to run again, and a run over a compacted
lake changes nothing.

## Leave out of git

Do not commit the production hostname, a device token, a `sha256:`
line from the host token file, or a TLS private key. Do not expose
plain HTTP for `serve` on a public interface. Do not point `serve`
at a network you do not control.

## Optional browser dashboard

After the base lake and proxy are configured, follow
[web-dashboard.md](web-dashboard.md) to register an OIDC client, map viewer
groups and enable `--web-config` (or the example unit's `LAMPI_SERVE_WEB_CONFIG`).
Use the actual configured public origin for the callback. Keep device auth on
`/v1`, preserve cookies and `no-store`, and omit callback query strings from
proxy access logs. A web-enabled loopback backend refuses an empty device-token
set. Verify a mapped viewer can browse metadata, an unmapped identity is refused,
and agents can still sync with their device tokens. Do not treat this check as
a replacement for the existing go-live/backup validation.
