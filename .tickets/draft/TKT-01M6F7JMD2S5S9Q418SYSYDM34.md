---
schema: 3
id: TKT-01M6F7JMD2S5S9Q418SYSYDM34
title: "Dashboard: wider screens, project and machine pickers"
type: epic
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

The owner, 2026-11-02, after the v0.11.1 deploy: the dashboard screens are narrow and fiddly. On Sessions the project is always elided and needs a hover, and there is no picker for project or machine as there is for harness. A picker would help target sessions, or look only at how to improve one project, in both its git and its path form.

A survey of every view (the investigator's report, 2026-11-02) found:
- `main` is capped at 1180px; only the profile editor uses `main.wide`.
- `.project-label` and `.session-name` ellipsize, and tables fix the first column at 22%.
- Machines show as raw machine ULIDs instead of device names, except on the conflict pages.
- Times print raw as RFC3339Nano.
- Sessions and Search take an exact project ID as free text, and neither has a machine filter.
- The read-token form takes session UIDs and bays as free text.

### Owner's choices (2026-11-02)

- **Scope of the first pass,** each its own PR in this order:
  1. the wide layout with no ellipsis;
  2. the project and machine pickers on Sessions, Search and Usage;
  3. pickers for the read-token scope.
- **Project data:** indexed columns from a catalog migration, not a warm in-memory cache. Each session's git remote and folder live only in `manifest_json`, with no index.

### Not in this pass

Cross-links: project labels, device pages, operations and review rows linking to filtered sessions. The owner left them out of the first pass.

### Alternatives

A warm in-memory cache built by a scan at start and kept current as heads arrive. Not chosen: the owner preferred SQL-backed prefix filters and picker lists. Rollback then means restoring the migration backup, not swapping the binary.
