---
schema: 3
id: TKT-01M3558E0M4CT3DWR4Z4GEY584
title: "Optional sidecars: terva raati/ and tasks archives"
type: task
status: done
status_reason: null
priority: low
due_on: null
labels:
  - area/adapter
  - phase/3-export
assignees: []
milestone: phase-3
parent: TKT-01M3558DZ6CBW2HB49WFFY20CP
origin: null
dependencies:
  - TKT-01M3558DPVCN661600P3F9HMB0
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:12Z
updated_at: 2026-09-23T19:07:23Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/be1a
  name: Cursor cloud agent
extensions: {}
---

## Description

Optional artifact families under TERVA_HOME beyond transcripts.

## Acceptance criteria

- [x] raati/ under TERVA_HOME is an optional artifact family beyond transcripts
- [x] tasks archives under TERVA_HOME are an optional artifact family beyond transcripts
- [x] Transcript ingest still succeeds when neither family is present

## Implementation plan

Extend the terva adapter and discovery. Do not add a harness or a sync path.

Discovery walks three optional trees under the terva home, beside sessions/**/*.jsonl and *.errors.jsonl:

- raati/raati-<digits>.json, the write-once deliberation record
- tasks/tasks-<session-id>.json, the session board including archived generations
- ext-data/tasks/tasks-<session-id>.json, the legacy board directory

A missing directory is an empty list. A name that is not one of those files is ignored. The session id in a tasks filename is the transcript meta id (terva's sess.ID). An unsafe id is the 16-hex sha256 prefix terva already uses.

A sidecar is an artifact on a session manifest, so the existing allowlist, ruleset, watermark, outbox, and manifest ACK apply. A tasks file attaches to the transcript whose meta id matches. A raati record attaches to the transcript named by swarm meta session_id for one of its agent ids, and otherwise to transcripts whose cwd is that agent's origin. An unlinked file is not a manifest and does not leave the machine. A project the allowlist refuses takes its sidecars with it.

The agent watches sessions, raati, tasks, and ext-data/tasks, one watcher each, with the same Match. Normalize still projects transcript_jsonl and errors_jsonl. raati_json and tasks_json stay in the CAS. A rewrite of those two kinds replaces the current artifact for that path and does not move the session head.

## Summary

Optional raati records and tasks archives are artifacts on the terva session they belong to. Discovery walks raati/raati-<digits>.json, tasks/tasks-<session-id>.json, and the legacy ext-data/tasks tree. A missing directory is an empty list, and a transcript still ingests when neither family is present.

A tasks file attaches by the transcript meta id, or by the 16-hex sha256 prefix terva uses for an unsafe id. A raati record attaches by swarm meta session_id for a seat's agent id, and otherwise by that agent's origin cwd. An unlinked file is not a manifest. The allowlist, ruleset v1, watermark, and outbox are unchanged. A rewrite of raati_json or tasks_json replaces the current artifact for that path and does not move the session head. Normalize still projects only transcript_jsonl and errors_jsonl.

The agent watches sessions, raati, tasks, and ext-data/tasks. make ci is green. Landed on cursor/terva-raati-tasks-sidecars-be1a as https://github.com/terva-sh/lampi/pull/23.

A nested ext-data/tasks directory created after the agent is already running can wait for the next sync before the watcher sees it. New boards are written under tasks/, which is watched directly. The conflict dashboard stays a separate ticket.
