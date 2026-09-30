---
schema: 3
id: TKT-01M3S9TCEE0TKQFGS4DSV15ZAF
title: "Search: a page built for recall"
type: epic
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T13:59:43Z
updated_at: 2026-09-30T13:59:43Z
created_by:
  id: agent:claude-code/cb0b017c
  name: ""
updated_by:
  id: agent:claude-code/cb0b017c
  name: ""
extensions: {}
---

## Description

The search page answers the query but is hard to read. The session is shown only as a UUID. One session's hits bury the others. The order follows the index, not time. Snippets are escaped JSON with their lines joined. Twelve form controls have equal weight. The chrome pushes the first result about 550px down. Matching is one literal phrase, so "web view" finds only those exact bytes.

The owner and an agent settled this design in a grilling session on 2026-09-30. Each decision below records the alternative that lost and why. The children carry the work, one PR each.

### What search is for

- The main job is recall: which session discussed X. The second is finding how something was used before, the exact command or output.
- Audit queries, such as every Bash error last week, belong in a separate tool, not in search. That rules out facets and filter-first layouts here.

### Ranked problems

1. The session is not identifiable.
2. One session buries the others.
3. Snippets are hard to read.
4. The order means nothing, and there is no count.
5. The form is slow to scan.

The owner added that rigid matching hurts: no multiple terms, no wildcards, no fuzzy matching.

### Decisions

- **Matching.** Several terms must all match within one event. `"quoted"` is an exact phrase and `-term` excludes.
  - Wildcards come later, only if multiple terms are not enough. The trigram index can serve GLOB/LIKE.
  - Fuzzy matching is deferred until something else needs ranking. It would need relevance ranking, which TKT-01M3F2PGH rejected, and the modernc driver cannot load spellfix1.
  - Matching a whole session, where terms appear in different events, is out of scope.
- **Short terms.** A term under 3 characters is checked with a substring test on the events that the 3+ character terms already matched. A query whose every term is short is refused.
  - Refusing any short term was the rejected alternative. It is predictable, but "web ui" and "go test -v" are common.
  - When this path reaches the 5 s read budget, the page names the cause and the fix (quote the phrase or add a term) instead of a generic timeout.
- **Zero hits.** Report whether each term matches alone, using one `LIMIT 1` probe per term, and show the active filters as removable chips.
- **Two result shapes.** The query layer gains a session-level shape next to the flat event list, and both are shared with MCP.
  - The sessions shape is the web default. The events shape stays the exhaustive one.
  - Grouping each page in the template was the rejected alternative. It is cheap, but the index re-inserts a live session in batches (`internal/recall/index.go:408-466`), so one session would show up as several groups.
  - The sessions shape reopens the "no sort of every match" rule from TKT-01M3F2PGH. A spike on a copy of the live index gates it, and page-level grouping is the fallback if it cannot stay well under 5 s.
- **Session header.** The header reads: project slug, then creation date, then about 80 characters of the harness's own title, falling back to the first user prompt.
  - Slug rule: the last two path parts of the git remote without `.git`. With no remote, the cwd's last folder name. With neither, "Unknown project". The full value shows on hover.
  - Creation date: the earliest recorded event time. With none, show the lake's first-received time, marked "received".
  - A dim second line shows the harness badge, machines, the hit count ("50+" when capped) and the UUID as a copy-on-click token.
  - Rejected: the first prompt only, and no title at all. Harness titles, such as Claude `ai-title`, are written to be recognizable.
- **Order in the sessions shape.** Sessions come newest first by their latest matching event, with no sort toggle. Hits within a session are in transcript order, first 3 shown. "Show all N" links to `view=events&session=…`.
  - Rejected: most hits first. Counts are capped and tie, and ordering by count makes the spike harder.
  - Rejected: expanding the hits in place with `<details>`. It makes the page and the query heavier.
- **Order and count in the events shape.** With `session=`, transcript order through `docs_session`. Without it, "most recently indexed first", labelled honestly on the page. No total count.
  - Rejected: recorded-time order. It needs a new index while TKT-01M3K45MX shrinks the index.
- **Paging.** `view=sessions|events` is a query-layer parameter. The sessions shape pages 20 sessions at a time, forward only. The smoke test's JavaScript-off 50-row check moves to `view=events`.
- **Snippets.** Keep real newlines, up to about 4 lines around the match, monospace for tool results. Do not strip `grep -n` prefixes, because they are recorded content.
  - A tool call shows its best-describing input field (`command`, `file_path`, `pattern`, `query`) next to the tool badge. When the match is in another field, show that field.
  - Stronger highlight in both themes. Every term is highlighted. A second window is shown when the terms are far apart.
  - Duplicate snippets are not collapsed, because which sessions read a file is recall information.
- **Form.**
  - Always visible: a wide text box with a syntax hint, a project picker built from the catalog, a harness picker, and a date range with presets.
  - Behind "More filters": event type, actor, and tool, with suggestions.
  - Removed from the web form: raw type, unlinked only, and tool errors only. The API keeps them, and a URL that sets them shows them as chips.
  - Active filters are shown as chips.
  - There is no machine filter, even though TKT-01M3FPP3 decision 6 promised one.
- **Chrome.** Hide the refresh bar on Search; refreshing today also redraws the form. Use a smaller header with no description line. The coverage line shrinks to "N of M sessions searchable", with details on hover, and sits beside the results. The help paragraph is replaced by the syntax hint.
- **Hit rows.** Each hit gets a copy action. With JavaScript on it copies to the clipboard. Without it, it links to `/sessions/{uid}/excerpt?gen=G&from=P&count=1`.
  - Time shows short per hit (`00:40 UTC`, with the date when it differs from the header), and the ISO time on hover.

### Constraints carried in

- TKT-01M3FPP3 decision 1: the web UI and MCP share one query surface, and a new capability goes into the query layer first.
- TKT-01M3F2PGM: an empty query shows the form and never lists the whole corpus.
- Every read has a 5 s budget.
- PRs stay small.
- The spike reads a copy of `search.db`, never the live lake on the dev host.
