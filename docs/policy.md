# Phase 0 policy

The decisions behind how a lake is hosted and what may reach it. Read it
when you need the reason for a default. Back to the
[documentation index](README.md).

These are the placement and handling decisions for the MVP lake.
Drew Short (`human:sothr`) locked them. This file records them. It does
not provision a host, terminate TLS, set up a disk, or add an object
store. The operator checklist is [vps-bringup.md](vps-bringup.md).
That file does not provision a host either, and it does not name one.

## Lake host

`terva-lampi serve` runs on a small VPS with local disk. The MVP lake
is that process and that disk. It is not a home NAS, and it is not an
S3-compatible store plus an index VM.

The binary speaks HTTP. On the VPS, put TLS in front of `serve`. Do
not expose plain HTTP on a public interface. The default bind stays
`127.0.0.1:8787` for a lake on the same machine. A reachable listener
is the TLS endpoint in front of that process.

Device tokens are the ones already implemented. `terva-lampi login`
writes a 256-bit token to a mode-0600 file and does not print it. The
agent reads `--token-file`, `LAMPI_TOKEN_FILE`, or `token_file` in
`config.json`. `serve --token-file`
hashes each token with SHA-256, rewrites that copy to `sha256:<hex>`,
and requires `Authorization: Bearer` on `/v1`. Copy the client's file
to the host before pointing `--token-file` at it. That manual copy
stays as the fallback once registration lands
([Registration and many lakes](#registration-and-many-lakes)).
`/healthz` stays open and returns no catalog data.

The tenant is Drew's machines. Do not point `serve` at a network you
do not control.

Example units under `deploy/` set no URL, so the agent falls back to
`http://127.0.0.1:8787` and a local lake works without a hostname in
git. On a machine that should upload to the VPS, set `LAMPI_SERVER`, or
`server` in `config.json`, to that host's HTTPS URL.
[vps-bringup.md](vps-bringup.md) is the order: encrypted disk, the
binary, the data directory, the device token, loopback `serve`, then
TLS.

## Registration and many lakes

Phase 0 said there was no enrolment API. On 2026-09-27 Drew reversed
that and locked the model in this section. The work is tracked under
TKT-01M3FHHB (Agent onboarding: registration codes, lake config, many
lakes). Registration is the way to add a device
([vps-bringup.md](vps-bringup.md#devices)). The manual token copy above
stays documented as the fallback.

### The model

- **The lake has an identity.** `serve` holds an ed25519 key list and a
  random lake id in its data directory, and records the lake id in the
  catalog. Backup and restore keep them. A new lake, or one upgrading
  from a release with no identity, gets one on its first start. A
  catalog that has recorded a lake id but lost its key does not start
  with a new one, because agents pin the key. Restore the key from a
  backup.
- **The lake publishes its keys.** `GET /.well-known/terva-lampi/keys`
  lists the lake id and each key with its status and validity window.
  The response is signed over a nonce the caller sends.
- **Devices have names.** Each token belongs to a named device with an
  id. The lake records which device made each request and can list and
  revoke devices by name. A device binds to one `machine_id`. A token
  from the old token file binds to the first `machine_id` it uploads
  under after the upgrade, and `serve devices unbind` resets that.
- **Registration codes.** The operator mints a code on the lake host.
  The code holds the lake URL, the lake id, the key that signed it, a
  one-time secret, an expiry (24 hours by default), and the signature.
  It does not hold a device token. The agent makes its own token and
  sends only its SHA-256 when it redeems the code at `/v1/register`.
  A code redeems once.
- **Base configuration.** A lake can publish a signed profile with
  `harnesses`, debounce values, `redaction`, and `projects` rules. The
  local `config.json` wins over every field. A local deny wins over a
  lake's allow. A lake's rules apply only to uploads to that lake.
  A profile cannot set the inventory mode; see
  [Off-box metadata](#off-box-metadata-the-inventory-report).
- **Many lakes.** An agent can report to several lakes. Each lake has its
  own token, allowlist, sync state and `machine_id`, so two lakes cannot
  join their data by machine. Top-level deny rules and redaction apply to
  every lake.
- **Entry.** The code is a secret. `register` reads it from stdin, a
  prompt, or a file, and never from a command argument, so `ps` cannot
  show it. The installer also accepts it in `TERVA_LAMPI_CODE`, so an
  operator can copy one line that installs and registers a machine. The
  installer passes it to `register` on stdin or in a private file, never
  as an argument, and the installed agent does not inherit it. That line
  leaves the code in shell history until it is used. The owner accepted
  this on 2026-09-27 (TKT-01M3J5HX8): a code is minted for one machine,
  works once and expires, so a used code in history is noise. Mint a
  code for the one-liner with a short `--expires`, and begin the line
  with a space so shells that ignore such lines leave it out of history.
- **Dashboard operators.** The browser dashboard is read-only for the
  `viewer` role. The `operator` role, mapped to its own IdP group, can
  also manage registration codes, so a signed-in operator can add a
  machine to the lake. The owner chose this on 2026-09-27
  (TKT-01M3J5HX9). A stolen operator session could therefore add a
  device, where a viewer session can only read. The mitigations: the
  separate group, operator routes that answer 404 to anyone else, a
  fresh IdP sign-in (OIDC `max_age`, `auth_time` within 10 minutes)
  before minting, short code lifetimes, and the audit log naming the
  operator. A device token can upload but cannot read the catalog.

### Routes without a token

Three routes answer without a token, and none returns catalog data.

| Route | Exposes |
|------|---------|
| `GET /healthz` | That `serve` is up |
| `GET /.well-known/terva-lampi/keys` | The lake id and its public keys |
| `POST /v1/register` | Whether a one-time secret is valid, and then the new device's id and base configuration |

Rate-limit the last two at the proxy and in `serve`. A failed
redemption is logged without the secret.

### What each piece protects

| If this leaks or is forged | The holder can | Bounded by |
|------|---------|---------|
| A registration code | Register one device, once, before it expires | Single use, the expiry, `serve register --revoke` |
| A device token | Upload as that device | `serve devices revoke` |
| A copied key list | Nothing new; it cannot sign a fresh nonce | The nonce signature |
| A code with the lake's URL and another key | Nothing; `register` refuses it | The key list fetched over TLS from that URL |
| A code signed by a retired key | Nothing; the lake and `register` refuse it | Key status |
| A forged code with the attacker's own URL | Receive the sessions that machine's allowlist admits | Only the fingerprint check |

The last row is the gap. Before it redeems a code, `register` shows the
URL, the lake id, and the key fingerprint, and asks for confirmation.
`serve identity` prints the same fingerprint on the lake host. Compare
the two, as you would an SSH host key. A run with no terminal must be
given the fingerprint.

After registration, the agent checks the pinned key on every `hello` and
on every base configuration it fetches, and pushes nothing to a lake
that does not prove it. It refuses a lake at the same URL whose key does
not chain to the pin.

### Rotating and retiring keys

`serve identity rotate` adds a key endorsed by the current one. Both
sign for an overlap, 14 days unless `--overlap` says otherwise. Each
agent reads the key list at start and hourly, follows the endorsement
from its pin, and moves the pin in `config.json` without registering
again. A machine that is off for longer than the overlap still follows
the chain when it comes back, because the retired key stays listed with
its endorsement. `serve identity retire KEY-ID` ends a key's window
early. SIGHUP, or a restart, publishes either change.

A code records the key that signed it. Once that key is retired, the
lake refuses the code, and so does `register` when it reads the key
list.

### If a key may have leaked

1. On the lake host, run `serve identity rotate`, so there is a key the
   leak did not touch.
2. Run `serve identity retire KEY-ID --compromised` for the leaked key,
   then send serve SIGHUP and take a backup.
3. Every agent still pinned to that key stops pushing and says the lake
   must be registered again. For each machine, revoke its old device
   with `serve devices revoke`, mint a code with `serve register`, and
   run `terva-lampi register --replace` there. The machine keeps its
   machine id and sync state, so nothing is sent twice.
4. Agents that had already moved to a newer key keep working. If the
   leak may be older than the last rotation, retire those keys as
   compromised too and re-register every machine.
5. Revoke any pending codes the leaked key signed. The lake already
   refuses them once the key is retired, and revoking them makes the
   list say so.

Someone holding the leaked key can still impersonate the lake to an
agent that has not seen the compromised mark, for example by sitting
between it and the lake. Registering again with a code checked against
`serve identity` on the lake host ends that.

### Audit

The lake appends to an audit log in its data directory for these events:
creating, redeeming, expiring and revoking a code; creating, binding,
unbinding and revoking a device; adding and retiring a key; saving and
deleting a profile; and every refused redemption. Backup covers the
log. It never holds a secret or a token. A profile event names the
revision it made; the catalog keeps every revision's document.

An event that records a catalog change is queued in the catalog in the
same transaction as the change, then appended to the log and cleared.
If the log cannot be written, the event stays queued and is appended
by the next write or the next start of `serve`. A crash between the
append and the clear can write a line twice; no event is lost. Events
that record no change, such as a refused redemption, take the same
queue, so the log keeps the order things happened in. If the catalog
cannot take the write, such an event is appended directly instead: it
may then land ahead of an event still queued, but it is not dropped.

### Upgrade order

Upgrade the lake before any agent. The new routes and `hello` fields are
additive, so `capture_protocol` stays 1 and an old agent keeps working.
A new agent that finds no key endpoint still syncs to a lake set by
`server` and `token_file`. `register` refuses that lake and says to
upgrade it.

### Windows

Windows has no SIGHUP. On Windows, adding or removing a lake takes
effect when the agent restarts.

## Bays

The registration epic ruled out multi-tenant lakes. On 2026-09-29 Drew
reopened that for access inside one lake and decided the model in this
section. The work is tracked under TKT-01M3N8KHW5 (Bays: segment one
lake and route sessions to a bay). Every read path honours bays
(TKT-01M3NNF27A): a signed-in viewer or operator reads only the bays its
IdP groups are granted, an admin reads all of them, and a device's
`/v1/stats` and `/v1/conflicts` cover only the bays it writes.

### The model

- **A bay is an access boundary.** A lake can be split into named bays.
  A dashboard user, a read token, or later an MCP client is granted some
  bays and not others, and reads only the sessions in them. Filtering
  search, export and views by bay comes with it. Separate retention,
  backup or encryption per bay is not part of the model: a separate lake
  gives that ([Registration and many lakes](#registration-and-many-lakes)).
- **A session can be in several bays.** Membership is a catalog row and
  never copies data. The blob store stays shared, with dedup across
  bays, and the derived views are not split by bay. Every read path
  checks membership in the catalog.
- **The default bay is an inbox.** Every lake has one. Data from before
  bays is in it, and a session that nothing places lands in it. It
  cannot be deleted and can have an alias. Only admins, and principals
  granted it by name, read it, because it holds sessions nobody has
  sorted. The aim is to keep it empty. An admin can turn it off. That
  applies at ingest only: a new session that nothing places is then
  refused and stays on its machine. The bay itself stays, keeps what is
  in it, and still takes a stored session that loses its last other bay,
  so no stored session is ever left in no bay.
- **The agent asks and the lake decides.** An agent requests bays with
  rules that match the way `projects` rules do. The lake records the
  request, then applies its own rules: hold a session for review, add a
  bay, or keep it out of one. A request for a bay the device may not
  write is recorded as refused and does not place the session. A session
  that its requests and the lake's rules leave with no bay lands in the
  default bay, or, when the default is off, is refused like any other
  unplaced session. A device is told only the bays it may write, because
  bay names can name clients.
- **Roles.** An admin reads every bay and manages bays, rules and
  grants. An operator adds machines and can be limited to some bays; it
  reads session content only in bays it is granted. A viewer reads only
  the bays it is granted. A registration code grants its device write
  bays within the minting operator's scope. Upgrading promotes no group
  to admin: existing viewers and operators are granted the default bay,
  so they read what they read before.
- **Every change is audited.** Membership changes, holds, releases,
  grants and bay changes go to the audit log through the same queue as
  the events in [Audit](#audit). A dashboard action that adds access
  needs a fresh IdP sign-in, as minting a code does.

### Routing

The lake routes every manifest (TKT-01M3NNF29W). It reads the bays the
manifest asks for, then its own rules, and only ever adds: nothing in
routing takes a session out of a bay.

- **Requests.** Each bay the manifest names, by id, name or alias, is
  recorded against the session with its outcome. One the device may not
  write, or one that does not exist, is refused and places nothing. The
  ACK lists the refused names without saying which reason applied, so a
  device learns no bay it was not given.
- **Rules.** A rule matches the fields a `projects` rule has, read the
  way an allow rule is, exactly, and can also name a harness. A rule on
  a harness alone is allowed. `cwd_prefix` covers the folder and
  everything under it, the problem TKT-01M3NQ83 records for allow, so a
  rule meant for one folder is a `cwd_hash`. A remote the agent could
  not read matches no `git_remote` rule. The actions are:
  - `add`: also put the session in the bay.
  - `deny`: keep the session out of the bay, whether it was asked for
    or another rule adds it. A deny does not remove a session already
    there, and it does not keep a session out of the default bay when
    nothing else places it.
  - `hold`: put a new session in the rule's bay and nowhere else, and
    record every bay it asks for as held. A stored session that a hold
    rule starts matching keeps its bays and is flagged for review; its
    later requests are held too. Hold wins over add and over requests.
- **Release.** An admin releases a held or flagged session in one step.
  Its held requests are resolved again against the device's grants as
  they are then, placed with the rules as they are then, and a held
  session leaves the hold bay unless it asked for it or a rule adds it.
  One left in no bay goes to the default. A hold released once does not
  return for the same bay.
- **Nothing places it.** A new session with no accepted request and no
  rule lands in the default bay. With the default off it is refused and
  nothing is stored. A manifest that sets `bay_aware` gets `409` with
  code `no_bay`. Any other gets `403`, which an agent from before bays
  already waits the full backoff on.

### Known limits

- `blobs/check` tells a device whether the lake holds a digest. A device
  that can guess a file's bytes can learn that some session in another
  bay holds them. Devices are the owner's machines, so this is recorded
  and not fixed.
- Reading the lake directory is reading every bay. Shell access to the
  lake host, a backup, or DuckDB pointed at `parquet/` is admin access.

## Retention

No TTL. Session bytes, catalog rows, and normalized projections stay
until `terva-lampi serve purge --session <uid> --yes` removes that
session, with `serve` stopped. Purge keeps a blob another session
names. A backup taken earlier still holds the bytes. This tree does
not delete by age.
Deleting a [bay](#bays) deletes no data. It removes the bay from each
session's membership, and only a session left in no bay moves to the
default bay; a session still in another bay stays there and does not
enter the inbox. Per-bay retention is a separate decision, not yet
made.

## Encryption at rest

Require the provider's volume encryption and/or LUKS on the VPS data
disk. CAS objects and `catalog.db` sit on that volume. There is no
application-level age wrapping for the MVP.

## Off-box raw

Allowlisted projects only. The gate is `projects` in `config.json`,
implemented in `internal/config`. This policy confirms that surface.

- Default deny. An empty `projects.allow` refuses every project.
- `projects.deny` wins over allow.
- A rule matches a cwd prefix on a path boundary, a cwd glob, a git
  remote, a git remote prefix on a `/` boundary, or terva's cwd hash
  (`hex(sha256(cwd)[:8])`). Every field set on the rule has to match.
  A rule with no fields matches nothing. A cwd glob that could match
  every directory is refused where rules load, in a profile or in
  `config.json`.
- A deny rule reads a doubt as a match. `cwd_prefix` and `cwd_glob`
  ignore case and are checked against the cwd with and without symlinks
  resolved; `cwd_prefix` is also tried with its own symlinks resolved. `cwd_hash` also
  matches the resolved cwd. `git_remote` and `git_remote_prefix` also
  match a session whose remote cannot be read. A cwd outside any repository has no remote
  and does not match it. Allow rules compare exactly.
- Git remotes are folded before comparison, so the scp and https
  spellings of one remote are one key. Only the remote named origin
  is copied onto the manifest, and only when the session cwd still
  has a `.git`. A URL remote loses its user part and password, except
  that an ssh URL keeps a bare login name.
- Project resolution reads files. `git` runs only for an admitted
  session whose root commit the reader cannot find, with config
  pinned so the checkout cannot make it run a program.
- The allowlist hash is not the lake's project id. `project_id` is
  the normalized origin URL and the repository root commit. See
  [protocol.md](protocol.md).

Cursor IDE and Cursor CLI exports use that same gate. The global IDE
database has an empty cwd and is refused by design, and so is a Cursor
session whose cwd cannot be read as an absolute local path. Neither case
adds a permit rule or a schema field. How the Cursor readers snapshot and
filter their databases is in
[Cursor sessions with an empty cwd](harnesses.md#cursor-sessions-with-an-empty-cwd).

Ruleset v2 still runs after the allowlist and before any request. A
hit is quarantined unless `redaction.upload_hits` is set, or unless
`terva-lampi quarantine allow` acknowledged that file's exact digest.
Leave `upload_hits` false. The manifest is scanned too, and a hit there is refused
whatever `upload_hits` says. Neither gate rewrites the raw file.

## Off-box metadata: the inventory report

The allowlist above gates raw bytes. The owner decided on 2026-09-28
(TKT-01M3M7M0PM) that an agent may also send metadata about projects
the allowlist refuses, so that a lake operator can see what a machine
captures and fix a misconfigured allowlist from the lake. The report
ships with TKT-01M3M7M0TH. Until an agent runs a release with it, the
agent sends nothing about a refused project.

The agent's `config.json` picks one of two modes. A lake profile cannot
set or clear it, and a profile that names it is refused.

- **SOCIABLE**, the default. The agent reports each project its
  harnesses hold, allowed or refused: the harness, the cwd, the cwd
  hash, the folded git remote, the session count, the total bytes, the
  newest session time, and the verdict with its refusal reason.
- **STRICT**, set with `"inventory": "strict"`. The agent reports
  allowlisted projects only, plus the total count and bytes of refused
  sessions, with no name, path, remote or hash. It still fetches and
  applies the lake's profiles.

Raw transcripts of a refused project stay on the machine in both
modes. The inventory is metadata, but a cwd or a remote can name a
project or a client, so the lake keeps only each device's newest
report, not a history.

The mode is local because it protects the machine's owner from the
lake. A lake that could turn SOCIABLE on could list the projects that
owner chose not to share. STRICT still sends refused counts so that an
operator can tell a few expected refusals from hundreds that point at a
wrong allowlist.

Alternatives the owner turned down: STRICT as the default, which leaves
the lake blind on every new machine; cwd hashes only, which the
dashboard cannot show or act on; and keeping the inventory on the
machine behind `agent refused`, which means reading logs on each host.

## Machine inventory

These roles run `terva-lampi agent` for the multi-host proof:

| Role | Process |
|------|---------|
| laptop | `terva-lampi agent` |
| desktop | `terva-lampi agent` |
| remote/cloud box | `terva-lampi agent` |

Names stay at the role. The lake they upload to is the VPS running
`terva-lampi serve`. An agent can also report to more lakes; see
[Registration and many lakes](#registration-and-many-lakes).
