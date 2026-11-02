---
schema: 3
id: TKT-01M6F7JMQYGY3WGGV55H0M3FR5
title: "Sessions, Search, Usage: project combobox and machine select"
type: task
status: done
status_reason: "Done in #252 (merged 82ba348); worked without a claim, which this records"
priority: normal
due_on: null
labels:
  - area/server
assignees: []
milestone: null
parent: TKT-01M6F7JMD2S5S9Q418SYSYDM34
origin: null
dependencies:
  - TKT-01M6F7JMM85DPVQ88YSC24TENC
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-11-02T22:55:13Z
updated_at: 2026-11-02T23:57:30Z
created_by:
  id: agent:claude-code/t3-43103b81
  name: ""
updated_by:
  id: agent:claude-code/t3-43103b81
  name: ""
extensions: {}
---

## Description

Step 2b of the epic. Depends on the migration that adds indexed project columns.

### Sessions filters

- **Project:** a combobox, an input with a datalist. It suggests the projects in scope by label, and the owners and folders above them. It accepts:
  - an exact project ID;
  - a git remote or remote prefix, such as `github.com/acme`;
  - a folder or folder prefix, such as `/home/sothr/.t3/worktrees`.

  The lake resolves the input to the matching indexed column. Matching follows the allow-rule semantics, as `config.ProjectMatch` does.
- **Machine:** a select of devices by name, which filters through provenance.

The picker lists come from SQL over the new indexes, limited to the scope's bays. They are cached briefly in memory and refreshed in the background, so a page render does not scan.

### Search and Usage

Apply the same two filters.

### API

Document the new query parameters in `web-api.md`. Keep `project=` working for links that already use it.

## Acceptance criteria

- [x] Project input suggests projects, owners and folders in scope and filters by exact ID, remote prefix or folder prefix
- [x] Machine select filters by device through provenance
- [ ] Search and Usage take the same filters; picker lists come from a short-lived cache, not a scan per render
- [x] web-api.md documents the parameters; project= keeps working

## Implementation plan

Catalog: PageRequest gains Remote, Folder and Machine. sessionFilters matches a prefix with col=v OR (col>='v/' AND col<'v0') on the catalog 22 indexes, so github.com/acme does not match acmex, and machine through EXISTS on provenance. ProjectChoices lists distinct remotes, folders of remote-less sessions, and machines in scope. Web: parsePage takes remote, folder, machine and match (a value starting with / is a folder, otherwise a remote, normalized with config.NormalizeRemote). sessionPickers caches choices per scope for a minute. projectOptions offers owners holding two or more repositories and folders where two or more project folders branch, then each repository and folder, at most 500. The Project box is an input with a datalist, and Machine a select by device name. project= still works and is kept as a hidden field when set.

## Notes

**agent:claude-code/t3-43103b81** at 2026-11-02T23:26:53Z

Shipped for Sessions only. Search and Usage read search.db, which holds only project_id, and Search drops out-of-scope hits after the query, so narrowing them that way empties pages. Split into TKT-01M6F9CM9YTMC0C0F67A51HH1J (Search and Usage: filter by remote, folder and machine), which needs an indexVersion bump. AC 3 is left unticked for that reason. Screenshots at 1700px checked: the Project box widened to flex 3 after the first render was too narrow. Tests: TestSessionPrefixFilters (catalog), TestProjectOptions and TestSessionsPickers (web). GOFLAGS=-mod=mod just ci passes.

**agent:claude-code/t3-43103b81** at 2026-11-02T23:57:30Z

draft to done: Done in #252 (merged 82ba348); worked without a claim, which this records

## Summary

Merged in #252 for Sessions. A Project box (match) suggests owners, branching folders, then repositories and folders, and filters by remote prefix or folder prefix on the catalog 22 indexes, segment by segment. A Machine select lists the machines in scope by device name. Choices are cached a minute per scope. The API gains remote, folder, machine and match. Search and Usage are split into TKT-01M6F9CM9YTMC0C0F67A51HH1J, which needs the search index to carry the columns; AC 3 is unticked for that reason. Review fixes: remote and folder together survive the form, and rows.Err is checked before choices are cached.
