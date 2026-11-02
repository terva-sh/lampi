---
schema: 3
id: TKT-01M4250SZ25TKAD2ZHE6APJFVS
title: "Agent: one refused manifest fails the whole sync pass"
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/agent
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-04T00:29:00Z
updated_at: 2026-10-04T00:29:00Z
created_by:
  id: agent:claude-code/5949504c
  name: ""
updated_by:
  id: agent:claude-code/5949504c
  name: ""
extensions: {}
---

## Description

Found while rehearsing Release v0.7.0 (TKT-01M424P3QSYP18KFCQWQ2TA2TK) on 2026-10-03.

`upload.Run` posts each session's manifest in turn and returns on the first error other than "no bay" (`internal/upload/upload.go`, the `postManifest` loop). So a single manifest the lake refuses for good, such as `400 harness "grokbot" is not supported` from a lake older than the agent, fails the whole pass. `sync` exits 1, and the running agent backs off for up to 5 minutes and tries again, hitting the same 400 every pass.

Today no session is lost: Grok Bot is last in `knownSources`, so every other harness posts first in each pass. That holds only while a new harness is added at the end and the work list keeps source order. A new harness placed earlier, or any other permanent 400 on one manifest, would hold back every session after it.

The no-bay path already shows the intended shape: record the session as pending and keep going. A permanent per-manifest refusal could be treated the same way, reported once like a refusal, without backing off the lake.

Reproduction: start a v0.6.0 lake, set `GROK_BOT_HOME` with one transcript and one allowed Claude session, run a v0.7.0 `sync`. The Claude session uploads, the pass ends with the 400, and the exit status is 1.
