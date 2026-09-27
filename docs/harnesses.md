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
| `terva` | `$TERVA_HOME/sessions`, plus optional sidecars `raati/raati-<nanos>.json` and `tasks/tasks-<session-id>.json` | terva's platform default |
| `claude` | `$CLAUDE_CONFIG_DIR/projects/**/*.jsonl` | `~/.claude` (on Windows, `%USERPROFILE%\.claude`) |
| `codex` | `$CODEX_HOME/sessions/**/rollout-*.jsonl` | `~/.codex` |
| `opencode` | A scheduled `opencode export` at `$XDG_DATA_HOME/opencode/export/**/*.json` | `~/.local/share/opencode` (on Windows, `%USERPROFILE%\.local\share\opencode`) |
| `cursor` | A read-only snapshot of `User/globalStorage/state.vscdb` and `User/workspaceStorage/<id>/state.vscdb` | `$XDG_CONFIG_HOME/Cursor` or `~/.config/Cursor` on Linux, `~/Library/Application Support/Cursor` on macOS, `%APPDATA%\Cursor` on Windows |
| `cursor-cli` | A read-only snapshot of `$CURSOR_CONFIG_DIR/chats/<workspace>/<session>/store.db` | `$XDG_CONFIG_HOME/cursor` on Linux when that variable is set, otherwise `~/.cursor` (on Windows, `%USERPROFILE%\.cursor`) |

- terva sidecars upload with a session the allowlist already permits.
- Codex `history.jsonl` is prompt history and is not a rollout, so it
  is not read.
- When OpenCode's `export/` has no JSON, the database file at that root
  is listed instead. `opencode.db-wal` is not read. An export uploads as
  `opencode_export_json`, and a re-export replaces the session head.
- The record shape for Claude, Codex, and OpenCode is internal to those
  adapters. Each pins a reader version and keeps keys it does not
  interpret.
- The Cursor IDE `state.vscdb` reader and the Cursor CLI `store.db`
  reader are separate corpora. They do not share a harness, a session,
  or a watermark.

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

### What sync prints

When `sync` refuses a `cursor` or `cursor-cli` session whose cwd is
empty, the stderr line names which of those cases it is.
