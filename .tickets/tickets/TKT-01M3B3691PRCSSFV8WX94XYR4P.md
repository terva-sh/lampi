---
schema: 3
id: TKT-01M3B3691PRCSSFV8WX94XYR4P
title: "Serve: per-request deadlines, check cap, drain, access log"
type: bug
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area/server
assignees: []
milestone: null
parent: TKT-01M3B35JS8J2F83FG4J7ZC0199
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-25T01:34:30Z
updated_at: 2026-09-25T01:34:31Z
created_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
updated_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
extensions: {}
---

## Description

The serve HTTP layer drops responses for slow uploads, rejects large first syncs, does not drain on shutdown, and logs nothing.

### Findings

- Write deadline shorter than the body. Proven. `internal/cli/serve.go:106-109` sets `WriteTimeout` 30s and `ReadTimeout` 2m. For HTTP/1.1 the write deadline starts once headers are read, so a body that takes more than 30s is stored and then the response is dropped. The client sees EOF, Caddy a 502, and the blob is retried whole.
- `blobs/check` capped at 1 MiB. Proven. `decodeJSON` (`internal/api/server.go:366`) limits every JSON body to 1 MiB, about 15.6k digests. The client sends every digest in one request, so a large first sync gets `400 invalid json: unexpected EOF` forever. An oversized body should be 413 with a message that says so.
- Shutdown does not wait. Proven. `srv.Serve` returns as soon as `Shutdown` starts, `runServe` returns, and `lake.Close()` closes the catalog under handlers still running. `WaitNormalized(context.Background())` can outlast systemd's stop timeout.
- Storage errors are 400. Read. `server.go:350` maps every `Catalog.Ingest` error to 400, so a busy or full disk reads as a client bug. Error bodies echo absolute paths (`server.go:160,228,326`).
- No access or error log. Read. After the startup lines nothing is logged. 5xx, 401, and normalize failures do not reach journald.

### Approach

`WriteTimeout: 0`, and per-request read and write deadlines through `http.NewResponseController`, sized from `Content-Length` for blob PUTs. A separate, larger cap for `blobs/check`, and 413 on any body over its cap. `runServe` waits for `Shutdown` to return before `Close`, and bounds the normalize drain; jobs persist, so a cut drain resumes at start. Map storage errors to 5xx and keep paths out of bodies. Middleware logs method, path, status, bytes, duration, and `X-Forwarded-For` as information only, plus the text of 5xx and normalize errors. `Authorization` is never logged.

## Acceptance criteria

- [ ] A PUT whose body takes longer than the old WriteTimeout gets its ACK
- [ ] blobs/check has its own cap and an oversized body is 413 with a clear message
- [ ] SIGTERM waits for in-flight handlers before the catalog closes, and the normalize drain is bounded
- [ ] Storage errors are 5xx and error bodies carry no absolute paths
- [ ] Each request logs method, path, status, bytes, and duration; Authorization is never logged
