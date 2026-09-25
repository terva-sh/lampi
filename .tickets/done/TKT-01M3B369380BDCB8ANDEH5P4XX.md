---
schema: 3
id: TKT-01M3B369380BDCB8ANDEH5P4XX
title: "Deploy: proxy body size, serve unit sandbox, backup notes"
type: task
status: done
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
updated_at: 2026-09-25T01:47:25Z
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

- [x] The nginx snippet sets client_max_body_size, request buffering off, and timeouts that fit a 32 MiB body
- [x] The serve unit carries the sandbox directives and serve still rewrites its token file under them
- [x] vps-bringup.md has a backup section naming the catalog, cas/sha256, and cas/logical, in order

## Implementation plan

### Approach

Docs and the example unit only. No Go behaviour changes.

- nginx snippet in docs/vps-bringup.md: client_max_body_size 40m (MaxBlobBytes is 32 MiB, protocol.go:27), proxy_request_buffering off, proxy_http_version 1.1, proxy_read_timeout and proxy_send_timeout 300s. serve's own ReadTimeout is 2 minutes (serve.go:107), so the proxy is not the tighter limit. Caddy: one sentence that reverse_proxy streams and has no default body cap.
- deploy/systemd/terva-lampi-serve.service: add the sandbox block. ReadWritePaths=/var/lib/terva-lampi. auth/devices.go:215 rewrites the token file with CreateTemp in its directory and a rename, so the token file's directory must be writable; say so in the unit and in vps-bringup.md. TimeoutStopSec=120 because Close drains the normalize queue. MemoryDenyWriteExecute is safe: the sqlite driver is modernc (pure Go), no cgo.
- vps-bringup.md backup section, in order: catalog via sqlite3 .backup, then cas/sha256, then cas/logical, plus tokens. partial/ is left out. normalized/ and parquet/ are derived: export (export.go:247) re-projects a session whose JSONL is missing and StoreEvents writes the JSONL and the parquet together. Restore drill compares catalog_* lines from terva-lampi status.
- deploy/README.md: mention the sandbox and the ReadWritePaths rule.
- internal/cli/packaging_test.go: pin the unit directives, the nginx directives, and the backup order so an edit cannot drop them silently.
- Run systemd-analyze verify on the unit (it is installed here).

## Notes

**agent:claude-code/eh1m** at 2026-09-25T01:47:12Z

### Verification

- `systemd-analyze verify` (systemd 255) passes on the unit, with ExecStart pointed at a built binary. `systemd-analyze security --offline=yes` rates it 3.0 OK. The container has no running systemd, so the unit was not started under real systemd.
- The sandbox was emulated with `unshare -m`: every mount remounted read-only except the data directory, as ProtectSystem=strict with ReadWritePaths does. serve started and rewrote a plaintext token in the data directory to a `sha256:` line. With the token file outside the writable path it failed at start with `open .../.token-...: read-only file system`. That error is what the docs quote. The emulation ran as root and does not cover seccomp or the capability set.
- The default build is dynamically linked (cgo net resolver) but maps no W+X memory; MemoryDenyWriteExecute is safe. The sqlite driver is modernc, pure Go.

### Changes from the proposed approach

- Added proxy_http_version 1.1 to nginx. Noted that serve's own ReadTimeout (2 minutes, serve.go:107) is tighter than the proxy's 300s.
- The backup uses sqlite3 .backup only; the test uses VACUUM INTO as the in-process equivalent, because the container has no sqlite3 CLI.
- Verified that normalized/ and parquet/ can be left out: export (export.go:247) re-projects a missing JSONL, and StoreEvents writes the parquet with it. TestBackupRestoresWithoutDerivedFiles restores from catalog + cas only and checks both come back and Counts match.
- The restore drill stops serve before taking the backup it restores, so the counts are stable; an upload between `status` and the stop is named as the one race.
- The later `terva-lampi serve backup` command is mentioned as tracked later work, not promised.
- No test covers a file over 32 MiB in cas/logical during restore; the test copies cas/logical when present, but its fixture is small.

## Summary

Landed on claude/elegant-feynman-eh1mdd-deploy in 3a4ae61. vps-bringup.md: the nginx snippet sets client_max_body_size 40m, proxy_request_buffering off, proxy_http_version 1.1, and 300s read and send timeouts; the Caddy snippet says it streams with no default cap; a Backup section (catalog via sqlite3 .backup, then cas/sha256, then cas/logical, then tokens; partial/ left out; export rebuilds normalized/ and parquet/) and a restore drill. The serve unit has the sandbox, UMask=0077, TimeoutStopSec=120, and the ReadWritePaths rule for a moved token file; deploy/README.md says the same. packaging_test.go: TestServeUnitSandbox, TestVPSBringupProxyAndBackup, TestBackupRestoresWithoutDerivedFiles.
