---
schema: 3
id: TKT-01M4250T0FQD7QKTDR26ZGNYX6
title: "Docs: document the grokbot harness"
type: chore
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/docs
  - area/adapter
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

#83 (Add the grokbot harness for Grok Bot transcripts) shipped in v0.7.0 with no operator docs. `docs/harnesses.md` lists Grok Build (`grok`) but not `grokbot`, and no page names `GROK_BOT_HOME` or `harnesses.grokbot.root`. The v0.7.0 release notes are the only description.

Add `grokbot` to the harness table and give it a section like Grok Build's: the path `agent-transcripts/<uuid>/<uuid>.jsonl`, the home (`GROK_BOT_HOME` or `harnesses.grokbot.root`, which wins, and no default), that the JSONL is the only artifact, that `store.db` and `conversation-blobs.db` are not read, session ids `grokbot:<uuid>`, and that it is a corpus separate from `grok`. Note that the manifest cwd is the transcript directory, so an allow rule needs a `cwd_prefix` under the Grok Bot home. Also say the lake must run v0.7.0 or later.
