---
schema: 3
id: TKT-01M3B369BNP6HM2AM4B78V99F2
title: "Client transport: timeouts, chunked puts, batched check, backoff"
type: bug
status: ready
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

The agent's HTTP client cannot move a large blob over a slow link, sends every digest in one check, never backs off, and will send a bearer token over plain HTTP.

### Findings

- Whole-request timeout. Read. `internal/upload/upload.go:177` sets `http.Client{Timeout: 60s}`, which covers the body. A 32 MiB PUT needs about 4.5 Mbit/s or it never finishes. `PieceBytes` and `ChunkBytes` exist but the CLI never sets them, so a failure restarts the blob.
- One check per sync. Proven. `upload.go:426-428` sends every digest in one `/v1/blobs/check`.
- No backoff. Proven. `internal/cli/agent.go:32,236-243` retries every 2s. A 401 lake got a hello every 2s with the error logged each time. `prepare`, the full scan, runs before `hello` (`upload.go:162` against `:179`), so each retry pays for the whole scan.
- Token over plain HTTP. Read. `upload.go:787,812-821` and `status.go:219` send `Authorization` to an `http://` URL on any host.

### Approach

Transport timeouts (dial, TLS handshake, `ResponseHeaderTimeout`) plus a progress deadline in place of `Client.Timeout`. Content-Range pieces of about 4 MiB by default. Batch checks at about 1,000. `hello` before `prepare`. Exponential backoff with jitter capped at 5 to 10 minutes; on 401 or 403, one clear log line and the cap. Refuse a token with `http://` unless the host is loopback. serve warns when `--token-file` is paired with a non-loopback bind.

## Acceptance criteria

- [ ] A 32 MiB blob uploads over a throttled link
- [ ] blobs/check is batched
- [ ] Retries back off exponentially with jitter, and a 401 logs once
- [ ] A token is refused over http:// to a non-loopback host
