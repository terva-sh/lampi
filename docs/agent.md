# The agent

Read this to understand what `terva-lampi agent` and `terva-lampi sync`
do on a machine: the order of the upload path, when the watch pushes,
how failures are retried, and which signals it answers. Back to the
[documentation index](README.md).

`sync` is one pass and exits. `agent` with no subcommand prints the
same discovery as `agent discover`, pushes the allowlisted sessions
once, then watches until SIGTERM. Both use the same upload path.

## The upload path

`sync` and `agent` run the same steps, in this order:

1. The project allowlist. See [Allowlist and redaction](allowlist-and-redaction.md).
2. The ruleset v2 scan.
3. The watermark plan.
4. The outbox.
5. The upload: PUT the blobs the lake does not have, then POST the manifest.
6. The manifest ACK, then the watermark commit and the outbox ACK.

A session whose files all match their watermarks is not read, scanned,
or posted. A second `sync` of the same files uploads nothing and posts
no manifest.

An append uploads the new tail only. The lake assembles it onto the
stored prefix. When the bytes before the tail scanned clean, only the
tail and 64 KiB before it are scanned. That window reaches further back
over a run a rule repeats, such as spaces before an AWS secret.

A file larger than 32 MiB is uploaded as chunks of at most that size.
The lake keeps the chunks and does not assemble one object past the
cap. The cursor does not move if the manifest POST fails. A file that
changed after it was hashed is not sent in that sync; the next one
sends it.

A file that cannot be read, or whose session id would sit on a line
too long to read, is skipped and named on stderr. The other files
upload.

## When the watch pushes

Growth calls the push once the watch has been quiet for 5s, and never
more than 30s after the first change. `agent.debounce` and
`agent.debounce_max` in `config.json` change those two, as Go
durations. `"0s"` pushes on every change.

The agent does not open a file whose size, mtime, and inode have not
moved since its last pass. The pass at start, and one every 6 hours,
reads and hashes every file. A rewrite that kept the size and mtime
therefore waits at most 6 hours.

A harness home that does not exist yet, terva included, is polled until
it appears. A watcher that cannot use fsnotify, at the inotify watch
limit or after a queue overflow, polls that tree and says so. On macOS
the agent polls by default. `LAMPI_WATCH=fsnotify` or `LAMPI_WATCH=poll`
overrides that.

## Retries

A failed push is tried again without waiting for the file to grow. The
first wait is 2s. Each further failure doubles the ceiling of a
jittered wait, up to 5 minutes, and a success resets it. Growth during
that wait does not start a push.

A 401 or 403 is logged once, naming the token file, and waits the full
5 minutes. A refusal or a skip is logged the first pass it appears, not
on every pass.

With more than one lake, each lake has its own outbox, backoff,
debounce ceiling, and 401 message. See
[Registration and lakes](registration-and-lakes.md#many-lakes).

## What the lake hears about the machine

Besides the uploads, the agent reports to each lake after each sync
and every minute in between: its release, the profile version it
applied, where its rules come from, its inventory mode, and the last
sync's counts or error. After a report it sends the inventory, the
projects its harnesses hold, when that has changed since the last one
the lake kept. `"inventory": "strict"` in `config.json` limits that to
the allowed projects and a count of the refused. See
[What leaves the machine](allowlist-and-redaction.md#what-leaves-the-machine).

## Signals and the pid file

The process writes `agent.pid` in the state directory while it runs. A
second agent on the same state directory exits and names the first
one's pid.

| Signal | Effect |
|--------|--------|
| SIGTERM | Drains the outbox, tries the upload path once more so a push in flight can finish, and exits. |
| SIGUSR1 | Pushes now, without waiting for the debounce. Unix only. It does not reload `config.json`. |
| SIGHUP | Reads the lakes again: their servers, tokens, and allowlists. Unix only. |

The harnesses map and the debounce are read when the process starts.
So are the lakes on Windows. Restart the agent to reload them.

The directory watch is the source of truth. SIGUSR1 only makes a push
happen sooner.

## The optional terva hook

[hooks/terva-post-tool-enqueue.sh](../hooks/terva-post-tool-enqueue.sh)
is the optional terva `post_tool_use` hook that sends SIGUSR1 to a
running agent. `make build` does not install it.
[Hook in deploy/README.md](../deploy/README.md#hook) is how to wire it.
If the hook never runs, the watch still uploads, and a down agent
uploads on its next start.

## What status prints

`terva-lampi status` prints:

- the machine id;
- one line per known harness, as
  `harness <id> enabled=<true|false> root=<absolute path or empty> source=<config|env|default>`,
  where `source` names which layer won;
- the outbox depth and a watermark summary;
- the last finished sync, and the last attempt (`ok` or `failed`) with
  the most recent error;
- the files that attempt skipped (`last_skipped`, with up to five named);
- the server and the token file, each with its `source`;
- whether `/healthz` answered, catalog counts from `GET /v1/stats`, and the
  lake's normalization: sessions by state, the job backlog, and the last
  failure (`lake_normalization`, `lake_normalize_jobs`,
  `lake_normalize_last_failure`).

With more than one lake, `status` prints one block per lake.
