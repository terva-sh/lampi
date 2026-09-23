---
schema: 3
id: TKT-01M3558DS31QVJ6Y6XH6HK8S4V
title: systemd user unit + launchd agent examples
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
  - phase/1-mvp
assignees: []
milestone: mvp
parent: TKT-01M3558DP1WFP9WNHEP6BDVGN3
origin: null
dependencies:
  - TKT-01M3558DDS1NVWKCC2S36TYPZV
  - TKT-01M3558D7M09TNZYEJ961WC4AK
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

Example unit files for `terva-lampi agent` once lake host is known.

## Implementation plan

Ship example user units under deploy/systemd and deploy/launchd. They run terva-lampi agent. Server URL and token file come from an EnvironmentFile (systemd) or EnvironmentVariables (launchd), defaulting to the loopback URL and ~/.config/terva-lampi/token. Comments say the lake host is still a Phase 0 decision and must not be invented. The agent reads LAMPI_SERVER and LAMPI_TOKEN_FILE, and the same values as --server and --token-file, so the examples do not require a config.json edit.

## Summary

Example user units are under deploy/systemd and deploy/launchd, with deploy/README.md. They run terva-lampi agent. The server URL and token file come from LAMPI_SERVER and LAMPI_TOKEN_FILE (systemd EnvironmentFile, launchd EnvironmentVariables), defaulting to http://127.0.0.1:8787 and ~/.config/terva-lampi/token. Comments say the lake host is still a Phase 0 decision. The agent reads those variables, and the same values as --server and --token-file. Nothing in deploy/ is installed by the build.
