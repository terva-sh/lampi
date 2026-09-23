---
schema: 3
id: TKT-01M3558DSRBXEECGW3HCFWX6AA
title: Optional lampi alias installer with neurobin warn
type: task
status: done
status_reason: null
priority: low
due_on: null
labels:
  - area/ops
  - phase/1-mvp
assignees: []
milestone: mvp
parent: TKT-01M3558DP1WFP9WNHEP6BDVGN3
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:11Z
updated_at: 2026-09-23T01:23:40Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/8319
  name: Cursor cloud agent
extensions: {}
---

## Description

Optional `lampi` symlink; warn if existing lampi looks like neurobin LAMP installer. Primary binary remains terva-lampi.

## Implementation plan

Add deploy/install-lampi-alias.sh. It symlinks terva-lampi to lampi under ~/.local/bin by default. If lampi already exists, it is left in place. A shell script, or a file that mentions neurobin, gets a warning that names the LAMP installer. The primary binary name does not change. Invoking the program as lampi already warns.

## Summary

deploy/install-lampi-alias.sh symlinks terva-lampi to lampi, defaulting to ~/.local/bin/lampi. An existing lampi is left in place. A shell script, or a file that mentions neurobin, gets a warning that names the LAMP installer. The primary command is still terva-lampi, and invoking the binary as lampi still prints that warning itself.
