# Read the lake from an agent

An agent that wants to know what past sessions did can read the lake's
normalized events from its own machine. It needs no shell on the lake
host. This page shows the owner how to give an agent that access, and
shows the agent how to use it. Back to the
[documentation index](README.md).

An agent can search the lake through MCP, or read events in bulk with
`terva-lampi query events`. The bulk
examples read every tool call. That data is useful when you decide which
tools a port or an emulation has to support.

## Before you start

- The lake serves the [dashboard](web-dashboard.md), and you are its
  admin.
- The agent's machine has `terva-lampi` installed, and can reach the
  lake over https.

## Give an agent a read token

1. On the dashboard, open **Read tokens**.
2. Mint a token:
   - **Label**: what it is for, such as `agent recall on laptop`.
   - **Reads**: select **normalized events** (`events:read`) only.
   - **Sessions** or **Bays**: limit the token to the sessions the agent
     needs. Leave both empty only when it needs the whole lake.
   - **Expires after**: the shortest time that covers the work.
3. Copy the token. The dashboard shows it once.
4. On the agent's machine, write the token to a file that only you can
   read:

   ```sh
   umask 077
   cat > ~/.config/terva-lampi/read-token    # paste the token, then Ctrl-D
   ```

5. Tell the agent where the file is. Do not paste the token into the
   agent's conversation or into a command line.

To stop the access, revoke the token on the same page.

## Let an agent search past sessions

`terva-lampi mcp` gives an agent the lake's recall tools over MCP. The
agent starts it as a local MCP server, and it sends each call to the lake
with the read token from the file. The agent's configuration names the
file, so the token never appears in it.

The lake replaces `terva-ext-session-search` for recall. That extension
searched one project on one machine. The lake holds every project from
every machine that uploads to it, so an agent finds work done elsewhere
too.

1. Give the agent's machine a read token, as above.
2. Add the server to the agent:

   - Claude Code, for every project:

     ```sh
     claude mcp add --scope user lampi -- terva-lampi mcp --token-file ~/.config/terva-lampi/read-token
     ```

   - Codex:

     ```sh
     codex mcp add lampi -- terva-lampi mcp --token-file ~/.config/terva-lampi/read-token
     ```

   If `config.json` lists more than one lake, add `--lake NAME` after
   `mcp`.
3. Start a new agent session, and ask it about earlier work, such as
   "find the session where we fixed the search WAL".

The agent gets three tools:

| Tool | What it does |
|------|--------------|
| `search` | Finds events by text, by filters such as `event_type`, `tool` and time, or both. Each hit names its session and position and links to the event in the dashboard. |
| `read_events` | Reads consecutive events of one session, for example from a few positions before a hit. |
| `copy_excerpt` | Renders up to 200 events of one session as plain text to paste into a new session or give to a person. |

The tools read only the sessions in the token's scope. Each call is
written to the lake's audit log under the token's label. Search text is
recorded only by its length. [MCP recall tools](web-api.md#mcp-recall-tools)
lists every argument and error.

The tools return transcript text as it was stored. A session can hold
text that reads like instructions, and the agent sees it as tool output.
Give the token only to agents you would let read those sessions.

## Read events

```sh
terva-lampi query events --token-file ~/.config/terva-lampi/read-token \
  --event-type tool_call --fields harness,session_id,tool.name,content_text \
  --out tool-calls.jsonl
```

- `--event-type tool_call` keeps tool calls. The other filters are listed
  in [Select events and fields](cli.md#select-events-and-fields).
- `--fields` writes only those paths, one JSON object per event.
- `--out` writes the file with mode 0600, and only when the whole stream
  arrived. Without `--out`, the events go to stdout.
- The command reads the lake named in `config.json` when there is only
  one. Otherwise, pass `--lake NAME` or `--server URL`.

A summary goes to stderr:

```text
terva-lampi: 4182 events from 311 sessions
```

The command exits nonzero if the stream stopped early. Do not use a
partial result as if it were complete.

## Count instead of copying

A tool call's `content_text` holds its full input, which can include
paths and other details from the session. When the question is which
tools are called and how often, ask the lake to count. Only the counts
cross the network:

```sh
terva-lampi query events --token-file ~/.config/terva-lampi/read-token \
  --event-type tool_call --count-by tool.name
```

```text
{"value":"Bash","count":2210}
{"value":"Read","count":1388}
```

Run it once per harness with `--harness` to split the counts. Free-text
paths such as `content_text` cannot be counted.

## On the lake host

An operator on the lake host can read the same events with
[`terva-lampi export`](cli.md#select-events-and-fields). It takes the
same filters and `--fields`, and selects the same events.
