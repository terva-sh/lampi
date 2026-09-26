---
schema: 3
id: TKT-01M3FBJMET80690HK2CJQD609M
title: Review, land and deploy the OIDC dashboard release
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/ops
  - area/auth
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:codex/deploy
  branch: t3code/web-session-lake-ui
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-8b6762d2
  commit: 88b36ad1fdabba1da0b4262d1d8fced625200795
  session: null
  claimed_at: 2026-09-26T17:18:02Z
  expires_at: null
archive: null
created_at: 2026-09-26T17:18:02Z
updated_at: 2026-09-26T18:14:44Z
created_by:
  id: agent:codex/deploy
  name: ""
updated_by:
  id: agent:codex/deploy
  name: ""
extensions: {}
---

## Description

Land the dashboard release after successful CI and model review, then upgrade the existing hosted lake with a protected pre-migration backup and verify real OIDC and agent behavior. Keep host coordinates and credentials outside the repository. The owner explicitly authorized merging and deployment. No shared-host power-off is authorized by this task.

## Acceptance criteria

- [x] All release code receives a published model review; findings are resolved or dispositioned while PRs remain open; CI passes and reviewed changes merge.
- [ ] A protected pre-migration backup, rollback binary/config and service-account configuration checks are completed before installation.
- [ ] The reviewed binary and OIDC/proxy configuration are installed; health, allowed/denied login, logout and device ingestion are verified.

## Implementation plan

Reduce review scope by landing the existing dependency-ordered OIDC/catalog foundation commits first, then review and land the remaining dashboard and operational changes through the original PR. Preserve history without force pushes. Read each published review and verify fixes before merging; synchronize both forges. Rebuild from the reviewed revision, then follow the external deployment runbook only after privileged execution and protected backup storage are available. Record actual verification separately from preparation.

## Notes

**agent:codex/deploy** at 2026-09-26T17:20:55Z

Owner confirms encryption on the underlying host volume; dedicated encryption can be future work. Owner will execute concrete privileged commands instead of sharing credentials. A read-only preflight script is prepared outside the repository and awaiting operator output. PR #5 separates the existing OIDC/catalog foundations so #4 can receive a complete review within the 256 KiB context cap. A future compressed age-encrypted backup ticket was filed as draft; no credentials or encryption settings were changed.

**agent:codex/deploy** at 2026-09-26T17:31:24Z

Foundation PR #5 merged as f4b799f618c372af5bc26d2f60d0ac17a3ce61c8 after CI success and model review 853 on 72bc1e81d02a750767953cdd8d8c0e6c4c1cfdf8. Two medium findings were fixed and dispositioned: owned login-attempt replacement at capacity, and bounded timestamp migration. A low cursor EOF finding was dispositioned to companion PR #4, which already contains that fix; no foundation-only deployment occurs. GitHub main was fast-forwarded. Integrated local make ci and full race suite passed after both fixes. Operator measurements show the uncompressed CAS/catalog does not fit in available space; streaming gzip estimate is 3,371,634,084 bytes. Use an offline compressed checkpoint on the confirmed encrypted host volume, with a 4 GiB reserve, catalog integrity check and full archive-vs-source byte comparison; this avoids deleting data or relying on hard-linked backups. Privileged operator execution remains required.

**agent:codex/deploy** at 2026-09-26T17:34:18Z

PR #4 review 856 (75b76fc7-0b3a-443e-9a35-788bb3b4490b) on 2b39c8a5b0d25447e41954f60d84ed9c216f28e5 found that explicitly empty limit/current/unlinked values bypassed the documented query validation. parsePage now distinguishes absent keys from present empty values. HTTP regressions cover empty and bare values returning 400 and explicit valid limit/true/false remaining accepted. Focused web package race tests passed. Review: https://git.local.sothr.com/terva-sh/lampi/pulls/4#issuecomment-14380

**agent:codex/deploy** at 2026-09-26T17:39:54Z

PR #4 merged as 4e932af6660b983d262c043138df5f303c9bb29f after successful CI and clean model review on 64d3f62ad5fe6c4d368bdda81bb31b1fed4f6744. Clean review: https://git.local.sothr.com/terva-sh/lampi/pulls/4#issuecomment-14385; finding disposition recorded in comment 14381. The previous note cited the wrong review-856 URL; correct URL is https://git.local.sothr.com/terva-sh/lampi/pulls/4#issuecomment-14378. Both forges main are synchronized. The merge tree equals the reviewed head. Rebuilt the external operator package from the merge revision, replaced obsolete uncompressed guidance with bounded gzip checkpoint steps and recovery guidance, verified shell syntax, synthetic backup/integrity/compare and capacity refusal, and refreshed checksums. Operator execution is pending; no live installation or backup has occurred. Browser authentication and post-upgrade capture remain unverified.

**agent:codex/deploy** at 2026-09-26T18:14:44Z

Operator deployment attempt passed package checksums but stopped at capacity guard before any service change. Current available space had fallen to 4,558,245,888 bytes versus required 8,678,091,606 bytes. Read-only inspection found 119 GiB in the configured Go build cache. Cleared only rebuildable compiler cache using mise exec -- go clean -cache; command succeeded. Available space is now 132,014,252,032 bytes. Both lake and capture services remain active. No lake data, repositories, module source cache, or deployment artifacts were removed. The unchanged checksummed operator script can now be retried, with the original 4 GiB backup reserve intact. Backup, installation and live browser verification remain pending.
