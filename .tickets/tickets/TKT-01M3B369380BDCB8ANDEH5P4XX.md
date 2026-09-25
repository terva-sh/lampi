---
schema: 3
id: TKT-01M3B369380BDCB8ANDEH5P4XX
title: "Deploy: proxy body size, serve unit sandbox, backup notes"
type: task
status: ready
status_reason: null
priority: high
due_on: null
labels:
  - area/ops
  - area/docs
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

The deploy examples need fixing before the VPS goes up.

### Findings

- nginx rejects uploads. Read from nginx defaults. The snippet in `docs/vps-bringup.md:163-175` has no `client_max_body_size`, which defaults to 1m. Any blob over 1 MiB is a 413. Request buffering and the 60s proxy timeouts also apply.
- The serve unit has no sandbox. `deploy/systemd/terva-lampi-serve.service` runs as its own user and nothing else is restricted. serve rewrites the token file, so any `ReadWritePaths` must include its directory.
- No backup section. `catalog.db` cannot be copied with `cp` while serve runs without risking a torn WAL. `cas/logical/` has to be in the backup: without it a file over 32 MiB is unreadable and its session answers 500.

### Approach

Add `client_max_body_size 40m`, `proxy_request_buffering off`, and 300s read and send timeouts to the nginx snippet. Say in the Caddy snippet that it streams and has no default cap. Add the systemd sandbox directives (`NoNewPrivileges`, `ProtectSystem=strict`, `ReadWritePaths=/var/lib/terva-lampi`, `ProtectHome`, `PrivateTmp`, `PrivateDevices`, the kernel protections, `RestrictAddressFamilies`, `UMask=0077`, `TimeoutStopSec`). Add a backup section: catalog first with `sqlite3 .backup` or `VACUUM INTO`, then `cas/sha256` and `cas/logical`. The CAS is append-only, so the later copy is a superset. `partial/` is not needed. `internal/cli/packaging_test.go` pins some of this text, so update it with the docs.

## Acceptance criteria

- [ ] The nginx snippet sets client_max_body_size, request buffering off, and timeouts that fit a 32 MiB body
- [ ] The serve unit carries the sandbox directives and serve still rewrites its token file under them
- [ ] vps-bringup.md has a backup section naming the catalog, cas/sha256, and cas/logical, in order
