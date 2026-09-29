---
schema: 3
id: TKT-01M3NS16SMX84HFZ1RV6187V2C
title: "Dashboard: show the stored normalize error on a failed session"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/normalize
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T05:08:37Z
updated_at: 2026-09-29T05:08:37Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

When a session's normalization fails, its page shows a "Failed" badge and
the line "Projection failed; operator logs hold the details." The
catalog already stores the failure, in `sessions.normalize_error`, set
through `Catalog.SetNormalizeError` and read by `Catalog.NormalizeError`
(`internal/catalog/catalog.go`). No dashboard page or API reads it.
Found on 2026-09-28 with a terva session in `zot` whose head is one
672 KB `transcript_jsonl`. The only way to learn why it failed was a
shell on the lake host.

Show the stored message on the session page's Normalization row, and in
the session API, when the state is failed.

### Who sees it

A projector error can quote the input it rejected, and a Go error can
name a lake path. That is probably why the page defers to operator
logs. Decide before building:

- **Admins only**, beside the raw view (TKT-01M3NKY2V3), since an admin
  can already read the raw bytes the message might quote.
- **Every viewer**, with the message bounded and control characters
  stripped, if a look at real messages in `normalize_error` shows they
  hold neither transcript text nor lake paths.

Check what the terva, claude and codex projectors actually put in the
message before choosing.

### Out of scope

Retrying normalization from the dashboard. `serve normalize --failed`
does that.

## Acceptance criteria

- [ ] A failed session's page and API show its stored normalize_error, bounded, to the audience chosen in this ticket
- [ ] The choice of audience and the messages checked to make it are recorded in this ticket
- [ ] Tests cover a failed session with a message, one without, and a viewer outside the chosen audience
