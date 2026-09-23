---
schema: 3
id: TKT-01M3558DYH3FVT06XBPKWAAMKH
title: Optional terva hook nudge (shipped example)
type: task
status: done
status_reason: null
priority: low
due_on: null
labels:
  - area/agent
  - phase/2-harness
assignees: []
milestone: phase-2
parent: TKT-01M3558DV5NHYZBFMPV1AVRNYQ
origin: null
dependencies:
  - TKT-01M3558DTHC06337Q51V6ERPTD
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:12Z
updated_at: 2026-09-23T16:52:55Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/2ecc
  name: Cursor cloud agent
extensions: {}
---

## Description

Polish and document the enqueue hook as a supported optional acceleration.

## Acceptance criteria

- [x] The enqueue hook is polished and documented as a supported optional acceleration
- [x] The filesystem watch remains the source of truth

## Implementation plan

Polish hooks/terva-post-tool-enqueue.sh into the supported optional post_tool_use acceleration. Document how to point terva at it from $TERVA_HOME/config.json, what SIGUSR1 does and does not guarantee, and that the filesystem watch (and the next agent start) stays the source of truth. Keep exit 0 when the agent is missing, the pid is foreign, or the signal cannot be sent. Recognize the agent by /proc/<pid>/exe on Linux, so the lampi symlink still counts when it points at terva-lampi, and by the invoked basename on other Unix. A command line that only mentions the name is not signalled. make build still does not install the script. Extend the packaging tests for that match.

## Notes

**agent:cursor/e4d5** at 2026-09-23T12:27:00Z

The example hook landed in #14 as hooks/terva-post-tool-enqueue.sh, under TKT-01M3558DTHC06337Q51V6ERPTD (Example terva post_tool_use enqueue hook). deploy/README.md describes it as an uninstalled example: it signals agent.pid with SIGUSR1 when that process is terva-lampi, and the directory watch stays the source of truth.

That example is not this ticket. The description asks to polish the hook and document it as a supported optional acceleration. Those two are still open, so this stays ready.

**agent:cursor/2ecc** at 2026-09-23T16:52:32Z

Polished hooks/terva-post-tool-enqueue.sh and the operator docs so the script is a supported optional post_tool_use acceleration. deploy/README.md now has the $TERVA_HOME/config.json wiring, what SIGUSR1 does and does not guarantee, and that the filesystem watch and the next agent start remain the source of truth. make build still does not install the script. On Linux the pid check is /proc/<pid>/exe, so a lampi symlink to terva-lampi is signalled and a command line that only mentions the name is not. Every miss still exits 0.

**agent:cursor/2ecc** at 2026-09-23T16:52:55Z

This note supersedes the last sentence of the 2026-09-23T12:27:00Z note. The polish landed and the ticket is done.

## Summary

hooks/terva-post-tool-enqueue.sh is the supported optional terva post_tool_use acceleration. The filesystem watch stays the source of truth: a missing agent, a foreign pid, or a hook that never runs still leaves upload to the watch or the next agent start, and the script exits 0. make build does not install it.
