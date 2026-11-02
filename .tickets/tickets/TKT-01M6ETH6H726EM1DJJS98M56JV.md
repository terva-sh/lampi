---
schema: 3
id: TKT-01M6ETH6H726EM1DJJS98M56JV
title: "Profile editor: merge selected allow rules under one prefix, with a preview"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - policy
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-11-02T19:07:14Z
updated_at: 2026-11-02T19:07:14Z
created_by:
  id: agent:claude-code/t3-43103b81
  name: ""
updated_by:
  id: agent:claude-code/t3-43103b81
  name: ""
extensions: {}
---

## Description

The owner asked on 2026-11-02 for a way to pick several allow rules in a profile, try a wider prefix that would replace them, and see before saving whether that prefix takes in every project they want and how many rules it saves. For example, one `cwd_prefix /home/sothr/.t3/worktrees` instead of a rule per project under `.t3/worktrees/<project>/`.

The doctor already offers a fold at the folders it finds on its own (`folder_fold`, `owner_fold`), but there is no way to choose the rules or the prefix yourself, and no way to see what a wider rule would admit beyond the rules it replaces.

### What changes

- The profile editor's allow list gets checkboxes and a Merge selected… action.
- The merge page proposes the selected rules' longest common folder, or common remote owner, and lets the operator edit the prefix. It then shows:
  - each existing rule the new one covers, which it removes, including rules that were not selected;
  - the projects the new rule would admit that no rule admits now, from the devices' inventories, marked as waiting in review or hidden;
  - the rule count before and after.
- A prefix that WideFolder refuses (a home directory, the folder that holds homes, `/`, a top-level folder) or a bare host is refused, with the reason.
- Apply hands the result to the editor's usual preview and save, so the devices it reaches show before anything is written.

## Acceptance criteria

- [ ] The editor can select several allow rules and open a merge page proposing their common folder or owner
- [ ] The merge page takes an edited prefix and lists the rules it replaces and the projects it newly admits
- [ ] Wide prefixes are refused with the reason; apply goes through the editor preview and save
- [ ] Tests and docs cover the merge page
