# Allowlist and redaction

Read this before you allow a project. It covers the two gates every
file passes before it leaves the machine: the project allowlist in
`config.json`, and the ruleset v2 secret scan. Back to the
[documentation index](README.md).

Do not upload a project whose transcripts you would not copy onto the
lake's disk in the clear. The scan catches common token shapes. It is
not a promise that every secret is caught.

Neither gate hides that a project exists. From the release with the
inventory report, the agent tells each lake which projects it sees
unless `config.json` sets STRICT mode. See
[What leaves the machine](#what-leaves-the-machine).

## The project allowlist

Raw bytes leave the machine only for a project that `config.json`
allows. The default is to refuse every project. The Phase 0 decision
behind that default is in [policy.md](policy.md#off-box-raw).

```json
{
  "server": "http://127.0.0.1:8787",
  "projects": {
    "allow": [
      {"cwd_prefix": "/home/you/src/foo"},
      {"git_remote": "git@github.com:terva-sh/lampi.git"},
      {"git_remote_prefix": "git@github.com:terva-sh"},
      {"cwd_glob": "/home/*/notes"},
      {"cwd_hash": "a1b2c3d4e5f60708"}
    ],
    "deny": [
      {"cwd_prefix": "/home/you/src/foo/private"}
    ]
  },
  "redaction": {"upload_hits": false}
}
```

`terva-lampi agent config` prints how many allow and deny rules are
loaded. `sync` names each refused session and exits non-zero.

`terva-lampi agent refused` answers "what would I have to allow?". It
reads every session the agent would read and prints one line per
refused project, most sessions first: the count, the harnesses, the
reason, a cwd and the git remote. A repository with many checkouts is
one line. The reason is a deny rule, an empty allow list, a session
with no cwd, or no matching allow rule. It uploads nothing and contacts
no lake, and `--lake NAME` picks which lake's rules to apply.

## What leaves the machine

| Mode | Allowed project | Refused project |
|------|-----------------|-----------------|
| SOCIABLE (default) | raw files, manifest, inventory row | inventory row |
| STRICT | raw files, manifest, inventory row | total count and bytes only |

An inventory row holds the harness, the cwd, the cwd hash, the git
remote, the session count, the total bytes, the newest session time,
and whether the project is allowed, with the refusal reason. The
inventory comes from the same scan as `agent refused`, so the two agree.
Raw bytes of a refused project never leave in either mode.

Set STRICT in `config.json`. A lake profile cannot change it:

```json
{"inventory": "strict"}
```

Any other value is an error, and the agent does not start, so a
misspelled `strict` never reports as SOCIABLE. A profile that names
`inventory` is refused.

The running agent sends the inventory to each lake after a sync, and
only when it differs from the last one that lake kept; one-shot `sync`
sends none. A lake from before the inventory answers 404, which the
agent says once. An agent from an earlier release sends nothing about
refused projects. The decision and
its reasons are in
[policy.md](policy.md#off-box-metadata-the-inventory-report).

### How a rule matches

A rule matches the session's cwd (a path prefix on a boundary, or a
glob), its terva cwd hash, its git remote, or a git remote prefix. Every
field set on a rule has to match. `projects.deny` wins over allow. An
empty rule matches nothing.

The cwd is the one the harness recorded, such as the terva meta line.
It is not the path of the transcript file.

An allow rule compares exactly, so a case or symlink variant of an
allowed path is refused.

A deny rule reads a doubt as a match:

- Its `cwd_prefix` ignores case, and it is checked against the cwd and
  the prefix as written and with symlinks resolved.
- Its `cwd_hash` also matches the hash of the resolved cwd.
- Its `git_remote` and `git_remote_prefix` also match a session whose
  remote cannot be read:
  the cwd is gone, or the checkout has no readable origin. A cwd that
  exists outside any repository has no remote, and a `git_remote` deny
  does not match it. Add a `cwd_prefix` to a `git_remote` deny to limit
  it to one tree.

The allow reading also decides which bays a session asks a lake for, in a
lake's `bays.rules`
([Asking for bays](registration-and-lakes.md#asking-for-bays)). Those
rules choose where a session goes on the lake. They do not admit a
session: only `projects.allow` does.

### Folder layouts: cwd_glob

A `cwd_prefix` names a path on one machine. A `cwd_glob` names a layout,
so one rule covers it on every machine and under every home directory:

- `*` matches any part of one folder name. `/home/*/notes` matches
  `/home/me/notes`, but not `/home/notes` or `/home/a/b/notes`.
- A folder that is exactly `**` matches any number of folders, none
  included. `/home/**/notes` matches all three. `**` inside a longer
  folder name, as in `proj-**`, is refused rather than read as `*`: it
  looks as if it reaches into subfolders, and it would not. Write
  `proj-*`, which already covers that folder and everything under it.
- Every other character is itself. There is no `?`, `[` or escape, so a
  path that holds one means what it says.
- Like `cwd_prefix`, it matches the folder it names and everything
  under it. `/home/*/.t3/worktrees` covers every worktree below it.

A pattern must start with `/`, has no empty, `.` or `..` folder, holds at
most four `**`, and needs at least one folder with no `*`, so that no
pattern matches every directory. A profile that breaks these rules is
refused, and so is a `config.json`: the agent does not start, rather than
run with a deny rule that denies nothing.

An allow `cwd_glob` compares exactly. A deny `cwd_glob` ignores case and
also tries the cwd with its symlinks resolved.

A lake or agent from before `cwd_glob` refuses a profile that uses it. An
older agent keeps its cached profile and reports the error. Upgrade the
lake first, then the agents, as for `git_remote_prefix`.

### Git remotes

Git remotes are folded before comparison, so
`git@github.com:terva-sh/lampi.git` and
`https://github.com/terva-sh/lampi` are the same remote. The folded
form is the host and the path. The scheme, login name, port and `.git`
are dropped, and case is ignored.

`git_remote_prefix` covers an owner, a group, or a whole host in one
rule. It is folded the same way and matches that remote or any under it
on a `/` boundary. `git@github.com:terva-sh` matches
`https://github.com/terva-sh/lampi` and
`ssh://git@github.com/terva-sh/group/app.git`, but not
`github.com/terva-sh-fork/app`. A remote with an empty, `.` or `..` path
segment matches no prefix. A prefix does not depend on where a checkout
lives, so one rule in a lake profile works on every machine. A
`cwd_prefix` rule names a path on one machine.

A lake or agent from v0.1.1 or earlier does not know
`git_remote_prefix`. An older lake refuses to load a `profiles.json`
that uses it. An older agent keeps its cached profile and reports the
error. Upgrade the lake first, then the agents.

When the session cwd still has a `.git`, the manifest records the
remote named origin. A URL remote loses its user part and password,
except that an ssh URL keeps a bare login name such as `git`. Any
other remote is ignored, so `git_remote` stays empty and a remote allow
rule does not match. Allow those projects by cwd or cwd hash.

The agent reads the remote, HEAD, and the root commit from files. It
runs `git` only for a session the allowlist admitted, and only when the
root commit is somewhere its reader does not follow. That call pins
config so the checkout's own settings cannot start a transport, a hook,
or another program.

### Project ids on the lake

The lake's `project_id` is not the cwd hash. It is the folded origin
URL joined with the repository's root commit. Two machines with the
same remote and root therefore share a project even when the absolute
paths differ. A shallow clone, or a checkout with no origin, has an
empty id and is not linked. See [protocol.md](protocol.md).

### Sessions with no cwd

An empty cwd matches no rule, so it is refused. The Cursor IDE global
database and some Cursor workspaces have one by design. See
[Cursor sessions with an empty cwd](harnesses.md#cursor-sessions-with-an-empty-cwd).

With more than one lake, each lake has its own allow rules and every
lake shares the top-level deny rules. See
[Registration and lakes](registration-and-lakes.md#many-lakes).

## The secret scan (ruleset v2)

Before a request is sent, ruleset v2 scans the file. v2 is the
high-signal shapes:

- AWS keys;
- GitHub, GitLab, Slack, Anthropic, OpenAI, Google, Stripe, npm, PyPI,
  Hugging Face, SendGrid, and DigitalOcean tokens with their fixed
  prefixes;
- PEM or PGP private-key blocks, from the BEGIN line through the END
  line.

It does not flag JWTs or generic `password=` or `api_key=` lines. Those
show up in ordinary transcripts, and a hit would quarantine the upload.
A key that is exactly one of the AWS documentation example keys, or
Slack's placeholder webhook, is not a hit. A key that only contains one
is.

The scan reads JSON string escapes as the text they stand for. A key
after an escaped line break, or a private key whose line breaks are
`\n` escapes, is still found. A Cursor value that the export holds as
base64 is scanned before it is encoded.

The scan also reads the manifest, which carries the cwd, the remote,
and the relpaths. A hit there is refused even with
`redaction.upload_hits` set, and cannot be allowed.

The manifest stamps `redaction.ruleset` as `v2` and `redaction.status`
as `scanned` when there are no hits. v1 missed keys inside JSON
escapes. Manifests it stamped stay on the lake as they are.

Neither the allowlist nor the scan rewrites the raw file.

## Quarantine

A hit is appended to `quarantine.jsonl` in the state directory (mode
0600) and is not uploaded. The log names the rule. It does not contain
the matched text. A record is added once per relpath, digest, and rule
set, not on every sync. A file whose digest already has a record under
the current ruleset is held again without being read or scanned, and
is still reported as quarantined.

- `terva-lampi quarantine list` prints the log.
- `terva-lampi quarantine allow <relpath|sha256>` acknowledges one
  digest. A file with exactly those bytes uploads, and the manifest
  status is `override` with the hit count. A file that changes is
  scanned and held again.

`redaction.upload_hits` is the override for every file. Leave it false.

## Training export

`terva-lampi export --format sharegpt` strips ruleset v2 matches from
the plaintext training fields. The CAS and `--format events` are not
rewritten. See [the export command](cli.md#export).
