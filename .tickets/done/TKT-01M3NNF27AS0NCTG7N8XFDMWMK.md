---
schema: 3
id: TKT-01M3NNF27AS0NCTG7N8XFDMWMK
title: "Bays: scope every read path by the caller's bays"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
  - area/catalog
  - area/server
assignees: []
milestone: null
parent: TKT-01M3N8KHW56GVZXHV0KBNSPEX1
origin: null
dependencies:
  - TKT-01M3NNF24AXMQ130VWR71QHQZ3
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T04:06:17Z
updated_at: 2026-09-29T16:51:03Z
created_by:
  id: agent:claude-code/7859b064
  name: ""
updated_by:
  id: agent:claude-code/7859b064
  name: ""
extensions: {}
---

## Description

Scope every read path by the caller's read bays, before any session can land in a bay other than `default`. Design in the parent epic TKT-01M3N8KHW5 (Bays: segment one lake and route sessions to a bay).

### Scope

- Recall, search, excerpts, transcripts, activity, overview counts and device views in `internal/recall` and `internal/web` filter by the caller's read bays, joining against catalog membership. An admin reads everything.
- The raw artifact routes check a read token's bay list as well as its session list. An admin in the browser still reads every session's raw artifacts.
- search.db may carry each session's bay set so FTS queries filter without a join. It is refreshed on a membership change without re-projecting the session.
- `terva-lampi export` on the lake host is admin-level and gains `--bay` (repeatable). The training export stays gated by the `projects` allowlist and also by `--bay`.
- A test lists every catalog query that returns session data and fails when one takes no bay scope. It must fail when a new unscoped query is added, not only check the ones known today.
- A single choke point (a query builder that requires a scope) was considered and not chosen, because it needs `internal/recall` refactored first. If that refactor turns out small, prefer it and say so in a note.

## Acceptance criteria

- [x] A viewer granted one bay sees no session outside it in any dashboard, recall or search response
- [x] A test fails when a query returning session data takes no bay scope, including one added later
- [x] export --bay narrows events and training exports

## Implementation plan

catalog.Scope (the zero value reads nothing) is a parameter on every catalog read of session data. The web guardRead builds it from the identity: AllBays for an admin, else GroupBays(groups, read). The API builds it with DeviceScope, from the device's write grants. Recall (events, excerpt, search) checks it, and export --bay builds InBays. Two tests guard it: an AST registry over exported Catalog methods (scope_test.go), and a route sweep over every web route literal (bays_scope_test.go).

## Notes

**agent:claude-code/7859b064** at 2026-09-29T16:51:03Z

Decisions and gaps:
- An AST registry, not a runtime check. Every exported Catalog method either takes a Scope or is listed in unscoped with a reason, so a new query is a decision, not an accident. A wrapper that filters rows after the query lost: it would still count out-of-scope rows in totals and pages.
- Mutation check: with Scope.where forced to 1=1, the route sweep reports 59 leaks across 14 routes.
- Coverage in search is a whole-lake count, so only admins see it.
- /v1/stats blanks LastFailure.SessionUID when that session is outside the device's scope.
- Main's conflict resolutions (#149) landed during the work. DivergentCopies takes both the scope and main's resolved filter, and the conflict count on the overview is both unresolved and in scope.
- Gaps: the operations page's storage bytes are lake-wide, and the device inventory shows project names to every viewer (it is agent-reported, not stored sessions). MCP is not built, so it has nothing to scope yet. The registry catches anything MCP adds to the catalog.

## Summary

PR stacked after TKT-01M3NNF24A. Every catalog read of session data takes a Scope. Web, API, recall, search and export build one from the caller, and two tests keep new reads from skipping it. Known gaps (storage bytes, inventory names) are in the notes.
