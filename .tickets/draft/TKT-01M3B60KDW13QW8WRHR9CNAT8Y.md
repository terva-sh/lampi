---
schema: 3
id: TKT-01M3B60KDW13QW8WRHR9CNAT8Y
title: "Pre-allowlist git reads: FIFO opens, ref confinement, Windows pins"
type: chore
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/agent
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
created_at: 2026-09-25T02:23:50Z
updated_at: 2026-09-25T02:23:50Z
created_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
updated_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
extensions: {}
---

## Description

Follow-ups from the review of terva-sh/lampi#68 (agent privacy, TKT-01M3B3698MKV82QSR0AR2CQYNS). Everything here is read before the allowlist decides, so a hostile checkout controls the input. None of it can run code. The worst case is a hang or a file read outside the checkout.

### Items

- `openRegular` in `internal/adapter/gitpack.go` stats the path and then opens it. A FIFO or device swapped in between is opened, and opening a FIFO for reading blocks. Open with `O_NONBLOCK` on Unix, check the mode with `fstat`, then clear non-blocking. That needs build-tagged files, and Windows keeps the current path.
- `originURL`, `headCommit`, and the `packed-refs` read in `internal/adapter/git.go` use `os.ReadFile`, which blocks forever on a FIFO planted in a checkout. Route them through the same regular-file open.
- `headCommit` joins a `ref:` target from HEAD without confining it to the git dir or the common dir. An absolute or `../` ref reads a file outside the checkout, and the content is used only if it is a hex commit. The same holds for an absolute `gitdir:`, `commondir`, or alternates path that feeds the pack reader. Reject a ref that leaves `refs/`, and confine the others or refuse them.
- `ResolveRoot` pins `core.hooksPath=/dev/null`. On Windows that path does not exist. Choose a per-platform empty path, or check that git treats a missing hooks path as no hooks.
- Deny `cwd_prefix` folds case but does not normalise Unicode. APFS treats NFC and NFD as the same name.

## Acceptance criteria

- [ ] No pre-allowlist read of a checkout file blocks on a FIFO or device; a test plants one
- [ ] A ref, gitdir, commondir, or alternates path that leaves the repository is refused; a test covers each
- [ ] ResolveRoot's hooks pin works on Windows
- [ ] Deny cwd_prefix compares NFC-normalised paths
