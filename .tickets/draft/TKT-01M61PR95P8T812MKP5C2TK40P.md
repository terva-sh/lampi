---
schema: 3
id: TKT-01M61PR95P8T812MKP5C2TK40P
title: "Harness filters refuse grokbot: allowlists name grok only"
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T16:51:05Z
updated_at: 2026-10-28T16:51:05Z
created_by:
  id: agent:claude-code/d4b24133
  name: ""
updated_by:
  id: agent:claude-code/d4b24133
  name: ""
extensions: {}
---

## Description

The protocol names the harness `grokbot` (`protocol.HarnessGrokBot`), and the live lake's overview lists grokbot sessions. The harness allowlists don't include it:

- `catalog.validHarness` (`internal/catalog/dashboard.go`) accepts `grok` but not `grokbot`, so the overview's link `/sessions?harness=grokbot` and the Activity harness filter refuse the request.
- `recall.validHarness` (`internal/recall/search.go`) has the same list, so search cannot filter by grokbot.
- The `sliceHarnesses` template function (`internal/web/pages.go`) omits it from the harness pickers.

Found while building the statistics pages (TKT-01M61NT6XQ5WA0XZ0AXZEYMHHG). One shared list derived from the protocol's harness constants would keep these from drifting again.

## Acceptance criteria

- [ ] /sessions?harness=grokbot lists grokbot sessions
- [ ] Search and Activity filter by grokbot
- [ ] One harness list shared by catalog, recall and the templates
