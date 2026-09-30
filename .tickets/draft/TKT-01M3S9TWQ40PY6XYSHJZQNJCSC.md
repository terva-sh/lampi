---
schema: 3
id: TKT-01M3S9TWQ40PY6XYSHJZQNJCSC
title: "Recall query: session filter in transcript order"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
assignees: []
milestone: null
parent: TKT-01M3S9TCEE0TKQFGS4DSV15ZAF
origin: null
dependencies:
  - TKT-01M3S9TWGZBF1SCKTKTE976ABH
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T13:59:59Z
updated_at: 2026-09-30T14:00:00Z
created_by:
  id: agent:claude-code/cb0b017c
  name: ""
updated_by:
  id: agent:claude-code/cb0b017c
  name: ""
extensions: {}
---

## Description

"Show all N in this session" from the grouped view lands on the events shape filtered to one session. The query layer has no session filter today: `parseSearch` rejects any parameter it does not know (`internal/web/server.go:396-404`). The decisions are in the parent epic, under "Order and count in the events shape".

- Add a `session=` parameter to the query layer. It is shared by the web API, the page and MCP, and composes with the text and the other filters.
- With `session=`, return hits in transcript order, earliest first, walking `docs_session(session_uid,pos)`. The spike must confirm the plan does not sort every match.
- Without it, keep the current order, and label it "most recently indexed first" on the page.
- No total count. The page says "50 shown, more available".

## Acceptance criteria

- [ ] The session filter works in the API, the page and the shared layer
- [ ] With a session filter, hits come in transcript order without sorting every match
