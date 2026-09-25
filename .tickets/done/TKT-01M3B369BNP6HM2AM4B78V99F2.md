---
schema: 3
id: TKT-01M3B369BNP6HM2AM4B78V99F2
title: "Client transport: timeouts, chunked puts, batched check, backoff"
type: bug
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area/agent
assignees: []
milestone: null
parent: TKT-01M3B35JS8J2F83FG4J7ZC0199
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-25T01:34:31Z
updated_at: 2026-09-25T02:48:07Z
created_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
updated_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
extensions: {}
---

## Description

The agent's HTTP client cannot move a large blob over a slow link, sends every digest in one check, never backs off, and will send a bearer token over plain HTTP.

### Findings

- Whole-request timeout. Read. `internal/upload/upload.go:177` sets `http.Client{Timeout: 60s}`, which covers the body. A 32 MiB PUT needs about 4.5 Mbit/s or it never finishes. `PieceBytes` and `ChunkBytes` exist but the CLI never sets them, so a failure restarts the blob.
- One check per sync. Proven. `upload.go:426-428` sends every digest in one `/v1/blobs/check`.
- No backoff. Proven. `internal/cli/agent.go:32,236-243` retries every 2s. A 401 lake got a hello every 2s with the error logged each time. `prepare`, the full scan, runs before `hello` (`upload.go:162` against `:179`), so each retry pays for the whole scan.
- Token over plain HTTP. Read. `upload.go:787,812-821` and `status.go:219` send `Authorization` to an `http://` URL on any host.

### Approach

Transport timeouts (dial, TLS handshake, `ResponseHeaderTimeout`) plus a progress deadline in place of `Client.Timeout`. Content-Range pieces of about 4 MiB by default. Batch checks at about 1,000. `hello` before `prepare`. Exponential backoff with jitter capped at 5 to 10 minutes; on 401 or 403, one clear log line and the cap. Refuse a token with `http://` unless the host is loopback. serve warns when `--token-file` is paired with a non-loopback bind.

## Acceptance criteria

- [x] A 32 MiB blob uploads over a throttled link
- [x] blobs/check is batched
- [x] Retries back off exponentially with jitter, and a 401 logs once
- [x] A token is refused over http:// to a non-loopback host

## Implementation plan

### Transport

- `upload.NewClient` builds the default client: dial 30s, TLS handshake 15s, `ResponseHeaderTimeout` 60s, `IdleConnTimeout` 90s, and no `Client.Timeout`. One package client is shared across syncs so the agent reuses connections.
- `doRequest` arms a stall watchdog (`Options.StallTimeout`, default 60s). Each read of the request body and of the response body resets it. It pauses between the last body byte and the response headers, which `ResponseHeaderTimeout` covers, so bytes still draining out of the kernel send buffer are not counted as a stall. A stall cancels the request with an error that says no bytes moved.
- Non-2xx answers become `*upload.StatusError` (same text as today) so callers can switch on the code.

### Pieces and resume

- `upload.DefaultPieceBytes` is 4 MiB. `sync` and `agent` set it. A body over it goes as Content-Range slices; chunks of a file over the cap go the same way.
- The lake keeps partial spans under `partial/` across requests but has no call that lists them. The client remembers, per server and digest, the offset after the last acknowledged piece, and the next attempt in the same process starts there. If the last piece answers `complete` false (the lake lost the partial), it sends the body again from byte 0 once. A 4xx on a piece forgets the offset.

### Check batches

- `postCheck` sends at most 1000 digests per request. A 413 halves the batch and retries that batch, down to one digest.

### Order

- `hello` moves before `prepare`, after the zero-manifest early return. A down lake fails the pass before the scan.

### Backoff

- `internal/cli/agent.go`: a `backoff` type, full jitter between 2s and a ceiling that doubles per failure, capped at 5 min, reset on success. rand is injectable.
- 401/403 (`StatusError`): one log line naming the token file, then the cap. Logged again only after a success or a different error.

### Token over http

- `upload.CheckTransport(server, token)` refuses a token with `http://` unless the host is `localhost`, 127.0.0.0/8 or ::1. Sync calls it first; the agent calls it at start; status and conflicts call it before sending the token (status prints the refusal on the catalog line).
- `serve` warns on stderr when `--token-file` is set and `--addr` is not loopback.

### Tests

Slow body over 32 MiB completes with a short stall timeout; a dead connection is detected (no read and no headers); check batches <= 1000 and 413 halves; range resume continues from the remembered offset and restarts from 0 when the lake lost the partial; backoff grows, caps, resets; 401 logs once; token refused over non-loopback http and allowed on loopback and https; serve warning.

## Notes

**agent:claude-code/eh1m** at 2026-09-25T02:47:43Z

### Design decisions that differ from the proposal

- The stall watchdog pauses between the last request byte and the response headers. Loopback and autotuned socket buffers let a writer finish long before the lake has read the body, so counting that drain as a stall gave false positives in testing. ResponseHeaderTimeout (60s) owns that wait.
- hello runs before prepare only when the allowlist permits at least one session. A pass the allowlist refuses whole still needs no lake and still stamps last_sync (TestStatusPrintsRefusalOnLastSync depends on it).
- Resume is in-process: the lake keeps partial spans but has no call that lists them, so the client remembers the next offset per server and digest. A new process starts at byte 0 and the lake overwrites the spans. If a resumed upload ends incomplete, the lake lost the partial and the client sends every piece again from 0 once.
- While a failed push waits to retry, file growth does not start a push; the retry timer picks it up. SIGUSR1 still pushes now. Without this, an active session would ask a down or 401 lake on every append, and the backoff would not hold.
- The token-over-http refusal also covers `conflicts --server`, which sent the token the same way.

## Summary

Landed on claude/elegant-feynman-eh1mdd-transport. internal/upload/transport.go adds NewClient (dial, TLS, response-header, idle timeouts; no Client.Timeout), a stall watchdog fed by body reads (Options.StallTimeout, default 60s), StatusError, Unauthorized, and CheckToken. Sync refuses a token over non-loopback http, runs hello before the scan when any session is allowlisted, batches blobs/check at 1000 and halves on 413, and sends bodies over PieceBytes as Content-Range pieces that resume in-process after the last acknowledged piece. sync and agent use 4 MiB pieces. The agent backs off with full jitter from 2s to 5 minutes, holds growth pushes while waiting, logs a 401/403 once naming the token file, and waits the cap. status and conflicts refuse the same way; serve warns for --token-file with a non-loopback --addr. Tests in internal/upload/transport_test.go and internal/cli/transport_test.go; docs updated.
