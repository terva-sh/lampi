---
schema: 3
id: TKT-01M3S9TWKZPRR114K462PD3YVD
title: "Session identity: project slug, creation date and title"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
  - area/catalog
  - area/normalize
assignees: []
milestone: null
parent: TKT-01M3S9TCEE0TKQFGS4DSV15ZAF
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T13:59:59Z
updated_at: 2026-09-30T13:59:59Z
created_by:
  id: agent:claude-code/cb0b017c
  name: ""
updated_by:
  id: agent:claude-code/cb0b017c
  name: ""
extensions: {}
---

## Description

The grouped view needs a header a person recognizes. The header is: project slug · creation date · about 80 characters of a title. The decisions are in the parent epic, under "Session header".

- **Slug.** From the git remote, keep the last two path parts without `.git` (`ssh://git@host:2222/terva-sh/terva.git` becomes `terva-sh/terva`). With no remote, use the cwd's last folder name. With neither, "Unknown project". Keep the full label for hover. Today `ProjectLabel` is the raw remote or cwd (`internal/catalog/dashboard.go:243`).
- **Creation date.** Use the earliest recorded event time in the session. With none, use the lake's first-received time, flagged so the page can mark it "received".
- **Title.** Use the harness's own title when it has one (Claude `ai-title`/`summary` events; Cursor's composer name, now in `extra`). Otherwise use the first user prompt. Collapse whitespace and cut at a word boundary near 80 characters. There is no title field today, so add a derived one and backfill it for existing sessions.
- **Second line.** Expose the machines on the result, which `SessionSummary.Machines` already has but `Hit` drops.

Pick where the derived fields live (catalog or index) after reading how normalization publishes a session. Record the choice and the alternative in a note.

## Acceptance criteria

- [ ] Hits carry slug, creation date, title and machines
- [ ] Existing sessions are backfilled
- [ ] The storage choice and its alternative are recorded in a note
