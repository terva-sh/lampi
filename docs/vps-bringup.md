# VPS lake bring-up

This is the operator checklist for the Phase 0 lake.
[policy.md](policy.md) is the decision. This file does not provision
a host, open a disk, or store a token. Copy the examples and edit
them on the machine.

The lake is `terva-lampi serve` on a small VPS with local disk. TLS
sits in front of that process. The process binds `127.0.0.1:8787`.
Agents set `LAMPI_SERVER` to the host's HTTPS URL. There is no TTL.
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
/var/lib/terva-lampi/catalog.db       SQLite catalog, plus WAL sidecars
/var/lib/terva-lampi/normalized/      one JSONL file per session
/var/lib/terva-lampi/parquet/         date=…/harness=… partitions
/var/lib/terva-lampi/tokens           host copy of device tokens
```

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

## Device token

On each machine that will upload, mint a token. `terva-lampi login`
writes a 256-bit token to `~/.config/terva-lampi/token` at mode 0600
and does not print it. `--token-file` chooses another path. The
token is not a command argument.

```bash
terva-lampi login
```

Copy that file to the VPS before you start `serve`. The copy is
yours to make. This tree does not log in to a host. The host path
is a different file from the client's. Keep the client's file. It
stays the secret.

`serve --token-file` hashes each token with SHA-256 and rewrites the
host copy to lines of `sha256:<hex>`, mode 0600. One file holds one
token per line, or a directory holds one file per device. There is
no enrolment API and no TTL.

`serve` replaces the file in that directory, so the service user has
to be able to write there. The example path is on the encrypted
volume, next to the catalog.

```bash
sudo install -m 0600 -o terva-lampi -g terva-lampi ./token /var/lib/terva-lampi/tokens
```

The agent reads `--token-file`, then `LAMPI_TOKEN_FILE`, then
`config.json`, then `~/.config/terva-lampi/token`. `serve` reads only
`--token-file`. It does not read `LAMPI_TOKEN_FILE`.

## Serve on loopback

The default bind is `127.0.0.1:8787`. Keep it. A non-loopback
`--addr` without `--token-file` is an error. With a token file,
still do not bind a public address. The reachable listener is the
TLS endpoint in front of this process.

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

`serve` writes one line per request to stderr, so
`journalctl -u terva-lampi-serve` shows them. A 200 `/healthz` is not
logged. `X-Forwarded-For` is logged as the proxy sent it.
`Authorization` is not logged. `systemctl stop` waits up to 20s for
requests in flight and 30s for the normalize queue, under systemd's
90s default. Jobs the drain did not reach run at the next start.

Nothing in `deploy/` is installed by `make build`.

## TLS in front

Put a TLS terminator on the public interface and proxy to
`127.0.0.1:8787`. Do not publish port 8787. Plain HTTP for the lake
stays on loopback. A public port 80 that only answers a certificate
challenge is not the lake. Do not proxy `serve` on that port.

Caddy, copy and edit:

```text
lake.example {
	reverse_proxy 127.0.0.1:8787
}
```

nginx is the same shape. The certificate paths are placeholders.
They belong on the host, not in git.

```text
server {
	listen 443 ssl;
	server_name lake.example;

	ssl_certificate     /etc/ssl/certs/lake.example.crt;
	ssl_certificate_key /etc/ssl/private/lake.example.key;

	location / {
		proxy_pass http://127.0.0.1:8787;
		proxy_set_header Host $host;
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

On each uploading machine, set `LAMPI_SERVER` to that HTTPS URL.
The agent unit reads `~/.config/terva-lampi/agent.env` (mode 0600).
The copy of that file in git keeps the loopback URL. Put the real
URL only in the file on the machine.

```text
LAMPI_SERVER=https://lake.example
LAMPI_TOKEN_FILE=/home/you/.config/terva-lampi/token
```

Restart the agent after either value changes. `GET /v1` requires
`Authorization: Bearer` and the device token. `/healthz` does not.

Laptop, desktop, and the remote/cloud box each run `terva-lampi
agent`. The names stay at the role. [policy.md](policy.md) lists
them.

## Backup

`terva-lampi serve backup --out DIR` copies the catalog, then
`cas/sha256` and `cas/logical`, then the token file with
`--token-file`. Run it as the service user. It runs while `serve`
runs. The catalog copy is `VACUUM INTO`, one consistent snapshot, and
the CAS is copied after it. A second run into the same directory
copies only new objects. Keep DIR on encrypted storage.
`terva-lampi serve fsck` re-hashes every object and exits non-zero
when one is bad.

## Leave out of git

Do not commit the production hostname, a device token, a `sha256:`
line from the host token file, or a TLS private key. Do not expose
plain HTTP for `serve` on a public interface. Do not point `serve`
at a network you do not control.
