---
schema: 3
id: TKT-01M3G44GCCNW58YJHZPPB105JD
title: "Lake identity: /v1/hello answers 200 when the body cannot be read"
type: bug
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/protocol
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-27T00:27:13Z
updated_at: 2026-09-27T22:37:50Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

In `internal/api/server.go`, the `hello` handler reads the body with `io.ReadAll(io.LimitReader(r.Body, maxHelloBytes+1))`. When that read fails, it calls `note(r, err)` and returns without writing a response. The client then gets an empty 200 where it should get an error, and an agent reads that as a hello with no proof.

The same bug in `/v1/register` was fixed during the onboarding review (TKT-01M3FHHBJ, review 974 finding-2). That fix writes the status and fixed message from the `bodyStatus` helper, which gives 400 "request body could not be read" for a plain read error, and keeps the error for the access log. The `/v1/hello` handler should do the same.

Found while fixing that finding; this code dates from TKT-01M3FHHBF (Lake identity).

## Implementation plan

Mirror the /v1/register fix: on a failed body read, hello writes bodyStatus's status and fixed message as a JSON error, and keeps the read error for the access log with note(). Test with a body that breaks off mid-read (iotest.ErrReader), as the register test does.

## Summary

/v1/hello now answers a body it cannot read with 400 'request body could not be read' (or 413/408 from bodyStatus), as /v1/register does, instead of an empty 200 an agent would read as a hello with no proof. The read error still goes to the access log. TestHelloSignsTheNonceAndStillTakesAnEmptyBody covers a body that breaks off mid-read, and fails without the fix.
