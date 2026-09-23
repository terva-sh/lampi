---
schema: 3
id: TKT-01M3558DTHC06337Q51V6ERPTD
title: Example terva post_tool_use enqueue hook
type: task
status: done
status_reason: null
priority: low
due_on: null
labels:
  - area/agent
  - phase/1-mvp
assignees: []
milestone: mvp
parent: TKT-01M3558DP1WFP9WNHEP6BDVGN3
origin: null
dependencies:
  - TKT-01M3558DDS1NVWKCC2S36TYPZV
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

hooks/terva-post-tool-enqueue.sh example — acceleration only; filesystem watch remains source of truth.

## Implementation plan

The agent already wakes its sync loop from an internal kick channel on watch events. Expose that as SIGUSR1 and write agent.pid in the state directory while the daemon runs. The example hook reads that pid, signals only when the process command line is terva-lampi, and exits 0 when no agent is running. The filesystem watch stays the source of truth; the signal only asks for a sync sooner.

## Summary

hooks/terva-post-tool-enqueue.sh is the example post_tool_use hook. A running agent writes agent.pid in the state directory. On Unix, SIGUSR1 wakes the same sync loop the filesystem watch uses. The hook sends that signal only when the pid's command line is terva-lampi, and exits 0 when no agent is running. The directory watch remains the source of truth.
