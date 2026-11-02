---
schema: 3
id: TKT-01M6F9CM9YTMC0C0F67A51HH1J
title: "Search and Usage: filter by remote, folder and machine"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
assignees: []
milestone: null
parent: TKT-01M6F7JMD2S5S9Q418SYSYDM34
origin: null
dependencies:
  - TKT-01M6F7JMQYGY3WGGV55H0M3FR5
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-11-02T23:26:53Z
updated_at: 2026-11-02T23:26:53Z
created_by:
  id: agent:claude-code/t3-43103b81
  name: ""
updated_by:
  id: agent:claude-code/t3-43103b81
  name: ""
extensions: {}
---

## Description

Split from TKT-01M6F7JMQYGY3WGGV55H0M3FR5 (Sessions, Search, Usage: project combobox and machine select). That ticket shipped the Project box and Machine select on Sessions, backed by catalog 22's indexed columns.

Search and Usage read the search index (`search.db`), whose documents carry only `project_id`. Search drops out-of-scope hits after the query (`recall.Search`, through `DashboardSession`). Filtering by remote or folder that way would leave pages mostly empty for a narrow project.

### Approach

Write the normalized remote and folder, and the posting machines, into the index's per-session rows, with indexes. Filter in `searchSQL` and in the usage and tool totals as `sessionFilters` does.

This changes `indexVersion` in `internal/recall/index.go`. The first start then deletes `search.db` and rebuilds it in the background, so the release notes must say search covers fewer sessions until the rebuild finishes.

### Alternative

Resolve the filter to a list of session UIDs in the catalog and pass it to the index. Not chosen: a folder like `.t3/worktrees` holds thousands of sessions, too many for an IN list on every page.

## Acceptance criteria

- [ ] The search index carries each session's normalized remote, folder and machines, indexed
- [ ] Search, Usage and tool stats take match, remote, folder and machine as Sessions does
- [ ] Release notes name the index rebuild
