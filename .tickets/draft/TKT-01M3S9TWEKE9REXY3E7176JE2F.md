---
schema: 3
id: TKT-01M3S9TWEKE9REXY3E7176JE2F
title: "Recall query: multiple terms, quoted phrases and exclusions"
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
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T13:59:59Z
updated_at: 2026-09-30T14:09:27Z
created_by:
  id: agent:claude-code/cb0b017c
  name: ""
updated_by:
  id: agent:claude-code/cb0b017c
  name: ""
extensions: {}
---

## Description

`ftsLiteral` (`internal/recall/search.go:142-146`) quotes the whole query as one FTS5 string, so "web view" matches only those bytes in that order. The owner said rigid matching hurts. The decisions are in the parent epic, under "Matching", "Short terms" and "Zero hits".

- Split the query into terms. Every term must match within the same event (FTS5 AND of quoted strings on the trigram index). A term in `"quotes"` is matched as an exact phrase, and `-term` excludes it.
- A term under 3 characters is checked with a substring test (`instr`) on the events that the 3+ character terms matched. A query whose every term is short is refused.
- If the short-term path runs out of the 5 s read budget, the error names the cause and the fix (quote the phrase or add a term). It must not be a generic timeout.
- With zero hits, report for each term whether it matches on its own, using one `LIMIT 1` probe per term that runs only on zero hits. Return that in the result shape so the web page and MCP can both show it.
- Keep the no-temp-b-tree query plans, and update the plan assertions.
- Keep the cursor fingerprint valid across the new syntax.
- Document the syntax in `docs/web-api.md`.
- Replace the search form's one-line hint with the new syntax (`words · "exact phrase" · -exclude`) in the same PR, so the page never advertises syntax the query does not support.

Out of scope: wildcards (a later ticket if still needed), fuzzy matching, and matching a whole session.

## Acceptance criteria

- [ ] web view matches an event holding both words in any order
- [ ] A quoted phrase matches only the phrase, and a minus term excludes
- [ ] A short term is post-filtered and an all-short query is refused
- [ ] Zero hits report per-term matches in the result shape
- [ ] Query plans still show no temp b-tree
- [ ] The form hint shows the new syntax only once this ticket's query change ships
