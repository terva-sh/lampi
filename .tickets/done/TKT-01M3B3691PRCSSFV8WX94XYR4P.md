---
schema: 3
id: TKT-01M3B3691PRCSSFV8WX94XYR4P
title: "Serve: per-request deadlines, check cap, drain, access log"
type: bug
status: done
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
updated_at: 2026-09-25T01:51:49Z
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

- [x] A PUT whose body takes longer than the old WriteTimeout gets its ACK
- [x] blobs/check has its own cap and an oversized body is 413 with a clear message
- [x] SIGTERM waits for in-flight handlers before the catalog closes, and the normalize drain is bounded
- [x] Storage errors are 5xx and error bodies carry no absolute paths
- [x] Each request logs method, path, status, bytes, and duration; Authorization is never logged

## Implementation plan

### Deadlines
`http.Server` keeps `ReadHeaderTimeout` and `IdleTimeout` and drops `ReadTimeout` and `WriteTimeout` to 0. A middleware in `internal/api` sets per-request read and write deadlines through `http.NewResponseController`: a floor plus the body size at a minimum rate. A blob PUT sizes from `Content-Length` (or `MaxBlobBytes` when absent). Every request gets a deadline, because with `WriteTimeout` 0 net/http does not reset the write deadline between requests on a kept-alive connection. The budgets are a field tests can shorten.

### Body caps
`decodeJSON` takes a limit, wraps the body in `http.MaxBytesReader`, and writes 413 on `*http.MaxBytesError`. `blobs/check` gets 8 MiB and at most 100000 digests; more is 413 naming the limit. Other JSON routes keep 1 MiB. A PUT whose body read failed (deadline, disconnect) is the client's, not a 500.

### Shutdown
`runServe` runs `Serve` in a goroutine, calls `Shutdown` on signal and waits for it (then `Close` on timeout), then `lake.Shutdown(ctx)` with a bounded normalize drain. The handler middleware counts in-flight requests and the api close waits for them before the catalog closes. Jobs left in the queue stay in `normalize_jobs` and resume at the next start; the log says how many.

### Errors
5xx bodies are a fixed message; the detail goes to the access log line. `Catalog.Ingest` errors are 500; the required-field checks Ingest did are moved in front of it in the handler so a bad manifest is still 400.

### Access log
`log/slog` text on stderr, one line per request: method, path, status, bytes, body bytes, duration, remote, `X-Forwarded-For` when present, and the error for 5xx. A 200 `/healthz` is skipped. `Authorization` is never read by the logger. Normalize failures log from the worker.

## Notes

**agent:claude-code/eh1m** at 2026-09-25T01:51:26Z

### Design notes

- Deadlines are set for every request, not only blob PUTs. With `WriteTimeout` 0, net/http does not reset the write deadline between requests on a kept-alive connection, so a route without its own deadline would inherit the last one. The budget is a floor (1m JSON, 2m blob PUT) plus the body size at 64 KiB/s, sized from `Content-Length` and capped at the route's max. The write deadline is 1m after the read deadline. A read deadline that passes while a handler still works cancels the request context (net/http background read), so the floor also bounds handler time.
- The `blobs/check` cap is 8 MiB and 100000 digests. The count is the number a client can batch to, and the 413 message names both. Client-side batching is the sibling ticket's.
- A body that stopped arriving (deadline, disconnect) is 408 or 400, not 500. `countingBody` records the read error so `fail` can tell the request's failure from the lake's.
- The `Catalog.Ingest` checks a client controls (machine_id, harness, native_session_id, artifacts present) were only in `Ingest`. They are now in `validateManifest` (merge.go, four lines) so a bad post stays 400 while every `Ingest` error becomes 500.
- The normalize drain is bounded. A job a worker already holds is not; it finishes before the catalog closes. Cancelling it mid-`StoreEvents` would need worker changes that overlap the worker recover() sibling.
- A 200 `/healthz` is not logged, so a probe does not fill journald. The format is `log/slog` text without the time key, because journald stamps lines. The startup lines keep their `terva-lampi serve:` form.
- The slow-body tests run with injected 100ms floors and 1 KiB/s (`Server.limits`). `TestServeHasNoFixedBodyTimeouts` pins the `http.Server` shape; the old 30s value is not reproducible in a unit test.

## Summary

Landed on `claude/elegant-feynman-eh1mdd-serve`. `serve` has no fixed `ReadTimeout` or `WriteTimeout`. `internal/api/access.go` sets per-request deadlines sized from the body, writes one slog line per request to stderr, and maps failures. `blobs/check` takes 8 MiB or 100000 digests, and any JSON body over its cap is 413. `serveLake` waits up to 20s for handlers, then drains normalize for up to 30s; queued jobs resume at the next start. 5xx bodies are a fixed message, and `Ingest` errors are 500. Tests: `internal/api/access_test.go`, `internal/cli/serve_test.go`, and a no-path check in `TestPutRejectsMismatchAndStorage`. Docs: README, architecture, protocol error table, vps-bringup.
