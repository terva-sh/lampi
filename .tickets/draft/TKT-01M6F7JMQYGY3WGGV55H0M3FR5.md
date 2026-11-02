---
schema: 3
id: TKT-01M6F7JMQYGY3WGGV55H0M3FR5
title: "Sessions, Search, Usage: project combobox and machine select"
type: task
status: draft
status_reason: null
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
updated_at: 2026-11-02T22:55:13Z
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

- [ ] Project input suggests projects, owners and folders in scope and filters by exact ID, remote prefix or folder prefix
- [ ] Machine select filters by device through provenance
- [ ] Search and Usage take the same filters; picker lists come from a short-lived cache, not a scan per render
- [ ] web-api.md documents the parameters; project= keeps working
