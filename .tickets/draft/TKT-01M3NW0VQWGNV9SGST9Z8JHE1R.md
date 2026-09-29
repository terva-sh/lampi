---
schema: 3
id: TKT-01M3NW0VQWGNV9SGST9Z8JHE1R
title: "Flaky under load: hangup reload outlives its test and panics"
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/ci
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T06:00:52Z
updated_at: 2026-09-29T06:00:52Z
created_by:
  id: agent:claude-code/58fb7d84
  name: ""
updated_by:
  id: agent:claude-code/58fb7d84
  name: ""
extensions: {}
---

## Description

`TestHangupReloadsTokensWithoutDroppingRequests` (`internal/cli/serve_hup_unix_test.go`) failed on a loaded runner in Forgejo CI for PR #145, actions run 1397, job 0. The failure has two stages.

1. The test waited for a reload and gave up: `serve_hup_unix_test.go:78: no reload`.
2. The test returned and its cleanup ran. The reload goroutine that `reloadOnHangup` started (`serve.go:399`) then ran `reloadDevices` (`serve.go:348`), which reached `api.(*Server).RecordDevices` → `flushAudit` → `catalog.(*Catalog).FlushAudit` on a nil `*Catalog` and panicked (`catalog/outbox.go:73`). The panic took down the whole `internal/cli` test binary.

The test passes locally with `go test -race -count=5 -run TestHangupReloadsTokensWithoutDroppingRequests ./internal/cli`, so it depends on timing. The PR that hit it touches no `serve` code.

### What to look at

- **Test lifetime.** The test should stop the hangup goroutine and wait for it before its cleanup closes the lake. An `Unwatch` or cancel, then a wait, would do it. Then a slow reload fails the test instead of panicking the package.
- **Timeout.** Consider whether the reload timeout is too tight for a loaded runner. On the failing run, `internal/api` took 212s and `internal/catalog` 208s.
- **Production path.** Check whether `reloadDevices` can reach a nil catalog outside the test, for example during shutdown.

## Acceptance criteria

- [ ] The hangup test stops and waits for its reload goroutine before cleanup
- [ ] A slow reload fails the test instead of panicking the package
