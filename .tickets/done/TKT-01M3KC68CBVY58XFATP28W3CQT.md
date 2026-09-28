---
schema: 3
id: TKT-01M3KC68CBVY58XFATP28W3CQT
title: Deduplication tile ignores prefix records and reads 1.00x
type: bug
status: done
status_reason: null
priority: low
due_on: null
labels:
  - area/server
  - area/ops
assignees: []
milestone: null
parent: TKT-01M3K45MQBRESGG5HEZZSZ3FDQ
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T06:45:42Z
updated_at: 2026-09-28T19:03:12Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

The Operations page's Deduplication tile reads 1.00× on the hosted lake
("74.3 GiB referenced, 74.3 GiB stored once") while stored blobs take
1.4 GiB. The real ratio is about 53×.

### Cause

`Catalog.ArtifactBytes` (internal/catalog/storage.go) computes "unique"
as the logical size of each distinct digest. Every version of a growing
transcript has its own digest, so each counts in full, even though since
prefix records (TKT-01M3K38AA) and `serve compact` a version shares its
bytes with the next. The tile (internal/web/operations.go) and the
`lampi_artifact_unique_bytes` metric divide by a figure the CAS no longer
stores.

### Directions

- Compare referenced bytes against the CAS component's measured disk use,
  which is what "stored" means to an operator. Measured and logical bytes
  differ slightly (block rounding, record files), so label it that way.
- Or have the storage sample sum physical bytes by resolving each digest
  to what the CAS holds for it (whole object, record, chunk list). That
  costs a walk the sampler already does.

## Summary

The Operations tile divides referenced bytes by the stored blobs' measured disk use (components.cas) instead of the distinct digests' logical sizes, and reads '<referenced> referenced, <cas> on disk'. On the hosted lake that is about 53x rather than 1.00x. The unique measure and lampi_artifact_unique_bytes stay, with their help text saying they are not the CAS's disk use.
