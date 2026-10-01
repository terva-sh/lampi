---
schema: 3
id: TKT-01M3W3QJ8PX9W1ZYX3MPM4KJ6R
title: "Spike: enroll many machines without minting a code for each"
type: spike
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/auth
  - area/agent
  - question
assignees: []
milestone: null
parent: TKT-01M3W3PZSDCB5FGKJS06MSZH65
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-01T16:11:02Z
updated_at: 2026-10-01T16:11:02Z
created_by:
  id: agent:claude-code/0f3154cf
  name: ""
updated_by:
  id: agent:claude-code/0f3154cf
  name: ""
extensions: {}
---

## Description

A registration code is a one-time secret with an expiry
(internal/regcode/regcode.go), so enrolling twenty machines means
minting twenty codes and carrying each one to its machine. For a
fleet installed by a script or an MDM that is the slow step.

Decide whether lampi should support enrolling many machines at once,
and how, without weakening what a one-time code protects:

- a code with a use count and a short expiry, every use recorded and
  revocable;
- `serve register --count N` writing N codes to a file for a script
  to hand out;
- leaving it as is and documenting a loop over `serve register`.

Record what each option costs if the code leaks, and how the profile
and bay a code assigns carry over.

## Acceptance criteria

- [ ] Each option is recorded with its cost if a code leaks
- [ ] The decision says how profile and bay assignment carry over
