---
schema: 3
id: TKT-01M3NQ8324NM3BQ51163ESQXE8
title: Allow on a folder project allows everything under it, home dirs too
type: bug
status: draft
status_reason: null
priority: high
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
created_at: 2026-09-29T04:37:26Z
updated_at: 2026-09-29T04:37:26Z
created_by:
  id: agent:claude-code/16ebd168
  name: ""
updated_by:
  id: agent:claude-code/16ebd168
  name: ""
extensions: {}
---

## Description

A folder project on the dashboard (one with no git remote) is allowed with a `cwd_prefix` rule, which covers that folder and everything under it (docs/web-dashboard.md, Allow). When the folder is a home directory, because a session was started there, the rule allows every project under that home on every device the profile reaches.

This happened on 2026-09-29. `default` had `cwd_prefix` rules for `/home/sothr`, `/Users/sothr` and `/Users/drewshort`, probably from Allows for home-directory sessions. When the workstation adopted `default` (TKT-01M3NMHDWR), 14 projects its local rules refused started uploading.

### Options

- Allow a folder project with `cwd_hash`, an exact folder, and offer "this folder and everything under it" as an explicit choice.
- Or keep `cwd_prefix`, but refuse or warn on a prefix that is a home directory or a parent of other projects the lake has seen, and show how many known projects it would cover.

The confirm page for Allow selected is where the second option would show.
