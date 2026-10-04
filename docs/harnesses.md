# Harnesses

Read this to find where `terva-lampi` looks for each harness's
transcripts, what it uploads, and why a Cursor session can be refused.
To turn a harness off or point it at another directory, see
[Harnesses in deploy/README.md](../deploy/README.md#harnesses).
Back to the [documentation index](README.md).

## Where each harness is read

`terva-lampi agent discover` lists the files the agent would read from
every enabled harness. A missing directory is skipped.

| Harness id | What is read | Default root when the variable is unset |
|------------|--------------|------------------------------------------|
| `terva` | `$TERVA_HOME/sessions`, plus optional sidecars `raati/raati-<nanos>.json` and `tasks/tasks-<session-id>.json` | `$XDG_STATE_HOME/terva` or `~/.local/state/terva` on Linux, `~/Library/Application Support/terva` on macOS, `%LOCALAPPDATA%\terva` on Windows. `ZOT_HOME` is the legacy name |
| `claude` | `$CLAUDE_CONFIG_DIR/projects/**/*.jsonl` | `~/.claude` (on Windows, `%USERPROFILE%\.claude`) |
| `codex` | `$CODEX_HOME/sessions/**/rollout-*.jsonl` | `~/.codex` |
| `opencode` | A scheduled `opencode export` at `$XDG_DATA_HOME/opencode/export/**/*.json` | `~/.local/share/opencode` (on Windows, `%USERPROFILE%\.local\share\opencode`) |
| `cursor` | A read-only snapshot of `User/globalStorage/state.vscdb` and `User/workspaceStorage/<id>/state.vscdb` | `$XDG_CONFIG_HOME/Cursor` or `~/.config/Cursor` on Linux, `~/Library/Application Support/Cursor` on macOS, `%APPDATA%\Cursor` on Windows |
| `cursor-cli` | A read-only snapshot of `$CURSOR_CONFIG_DIR/chats/<workspace>/<session>/store.db` and `$CURSOR_CONFIG_DIR/acp-sessions/<session>/store.db` | `$XDG_CONFIG_HOME/cursor` on Linux when that variable is set, otherwise `~/.cursor` (on Windows, `%USERPROFILE%\.cursor`) |
| `grok` | `$GROK_HOME/sessions/<encoded-cwd>/<uuid>/updates.jsonl`, and `summary.json` in that directory | `~/.grok` |

- terva sidecars upload with a session the allowlist already permits.
- Codex `history.jsonl` is prompt history and is not a rollout, so it
  is not read.
- When OpenCode's `export/` has no JSON, the database file at that root
  is listed instead. `opencode.db-wal` is not read. An export uploads as
  `opencode_export_json`, and a re-export replaces the session head.
- The record shape for Claude, Codex, OpenCode, and Grok Build is
  internal to those adapters. Each pins a reader version and keeps
  keys it does not interpret.
- The Cursor IDE `state.vscdb` reader and the Cursor CLI `store.db`
  reader are separate corpora. They do not share a harness, a session,
  or a watermark. Grok Build is its own corpus, harness `grok`. A
  Cursor session that used a Grok model stays on `cursor` or
  `cursor-cli`.

## Grok Build

`GROK_HOME` wins. When it is unset, the home is `~/.grok`. There is
no XDG fallback. A relative `GROK_HOME` is used as given.

A session is `sessions/<encoded-cwd>/<uuid>/`. The group directory
name is the URL-encoding of the cwd. Hex digits in that encoding are
uppercase, and ASCII letters, digits, and `-_.~` stay as themselves.
When the encoding is longer than 255 bytes, the name is slugify of
the path's leaf, a hyphen, and the first 16 hex characters of
BLAKE3(cwd). That directory holds a `.cwd` file with the original
path.

`updates.jsonl` is the transcript and the source of truth. The native
session id is the UUID directory. A directory name that is not a UUID
is not a session. `summary.json` beside the transcript is the
companion: cwd is `info.cwd`, the title is `generated_title` when
that is set and `session_summary` otherwise, and the model is
`current_model_id`. A directory that has only `summary.json` is not
a session.

Sync uploads those two files. `updates.jsonl` is kind
`transcript_jsonl`. `summary.json` is kind `summary_json`.
`chat_history.jsonl` is not the transcript. It and the other files
in the session directory stay on the machine.

Workers project `updates.jsonl` onto schema_version 1. The methods
they read are `session/update` and `_x.ai/session/update`. Consecutive
`user_message_chunk`, `agent_message_chunk`, and `agent_thought_chunk`
lines of the same kind coalesce into one message. A change of
`promptId` or `promptIndex` starts a new message. `tool_call` becomes
a `tool_call`. `tool_call_update` becomes a `tool_result` only when
its status is `completed` or `failed`. Any other method, and any other
`sessionUpdate` including other xAI extensions, is skipped.
`summary.json` and `chat_history.jsonl` are not projected.
`session_id` is `grok:` plus the native UUID. The pinned reader
version is `1`. The record has no confidence field. The Cursor IDE
reader version is `2`, and its confidence is `low`.

Every harness passes the same project allowlist before anything leaves
the machine. See [Allowlist and redaction](allowlist-and-redaction.md).

## Cursor sessions with an empty cwd

The `projects` allow and deny rules are the same for every harness.
An empty cwd matches no cwd prefix, no cwd hash, and no git remote,
so default deny keeps the export on the machine.

### Cursor IDE

The Cursor IDE global database (`User/globalStorage/state.vscdb`) has
an empty cwd, so that export is refused by design. Current Cursor
builds keep chat bodies in the global database's `cursorDiskKV` table,
so a read of the workspace database alone misses type 1 and type 2
bubbles. The export copies the global database when
`composer.composerHeaders` names at least one composer. One sync
copies it at most once and shares that copy across workspaces. Each
workspace selects, in SQL, only the `cursorDiskKV` rows of the
composers it names.

A snapshot copies the database and its WAL, not the `-shm` index.
When a checkpoint moved the files during the copy, it copies again, up
to five times. `PRAGMA quick_check` must pass on the copy, which is
then opened read-only. The workspace database uses the same snapshot.
The reader does not open a live database.

The merge puts matching `cursorDiskKV` rows into the workspace document
field `cursor_disk_kv`. Membership is `allComposers[].composerId` on
the ItemTable key `composer.composerHeaders`. `composer.composerData`
is the older workspace list and is not the registry. A composer listed
only on `composer.composerData` is not merged. A missing global file
adds nothing to the document. If the copy or the open fails, the
workspace export fails. The global export itself still does not leave
the machine.

`sync` asks the allowlist before it builds an export, so the global
database and a refused workspace are not copied or exported at all.
`sync` still names them as refused.

The Cursor IDE pinned reader `Version` is `2`, and the document field
`harness_version` is that string. `confidence` is `low`. The native
session id stays `workspace/<id>`. `terva-lampi export --format events`
writes `session_id` as `cursor:workspace/<id>`.

A workspace database takes its cwd from the folder URI in the sibling
`workspace.json`. A URI with no local path, such as `vscode-remote`, is
an empty cwd as well, and that workspace stays on the machine.

Normalize promotes a bubble tool out of that document. A
`toolFormerData` name and call id become a `tool_call`, and a result
string becomes a `tool_result`. A missing name or call id stays on
`extra`. On a `composerData:` row, `usageData` becomes a sibling
usage event only when it has a numeric `costInCents` or a recognizable
token count, and `latestConversationSummary` becomes a sibling
compaction event only when a summary string is present. Bubble
`usageData` and `tokenCount` stay on `extra`. The rules are in
[architecture.md](architecture.md#flow). The adapter `Version` stays
`2`. ShareGPT already includes that call.

### Cursor CLI

A Cursor CLI chat takes its cwd from the `cwd` field of the sibling
`meta.json`, and only when that value is an absolute path. A missing
file, a relative path, or a file URI leaves the cwd empty, and the
allowlist refuses the export. The workspace directory name is a hash,
not a path.

A session that an ACP client starts, such as an editor that drives the
Cursor agent over the Agent Client Protocol, is
`acp-sessions/<session>/store.db`. It has the same tables as a chat
and the same `meta.json` beside it, and it takes its cwd the same way.
Its session id is `acp-sessions/<session>`, so a chat and an ACP
session with the same uuid are different sessions. A directory under
`acp-sessions/` that holds only `meta.json` is not a session: the
client opened it and wrote nothing, and nothing is reported for it.

The export is the whole database, rebuilt on every change. The agent
therefore exports a Cursor CLI session, chat or ACP, only after its
`store.db` and `store.db-wal` have gone 5 minutes unwritten. It counts
a held session in the inventory, prints `held N until HH:MM:SS` on the
pass summary, and runs a pass when that time is up. `terva-lampi sync`
does not wait.

An export larger than the lake's object cap (32 MiB) goes up as a
chunk list. The reader cuts it between blob rows, after a row picked by
its id, so a chunk holds about 32 rows and at most 4 MiB. Blob ids are
content hashes and rows are in id order, so a new blob changes the
chunk it lands in and the first chunk, which holds the meta rows. The
other chunks keep their digests, the lake already has them, and the
upload sends only the changed ones. On a real 284 MB export, adding one
blob sent 1 chunk of 435, 3 MB. The lake stores the chunk list, not a
second whole copy. The document is the same bytes as before, so
normalize, export and older lakes read it unchanged.

The export is still built whole in memory. A session whose `store.db`
and WAL together pass 256 MiB is not exported: the pass prints a
`skipped` line with the session and its size, and the session stays on
the machine.

### What sync prints

When `sync` refuses a `cursor` or `cursor-cli` session whose cwd is
empty, the stderr line names which of those cases it is.
