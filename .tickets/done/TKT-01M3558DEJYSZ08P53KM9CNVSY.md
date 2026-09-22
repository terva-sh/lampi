---
schema: 3
id: TKT-01M3558DEJYSZ08P53KM9CNVSY
title: One-shot sync uses outbox + watermarks end-to-end
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area/agent
  - phase/1-mvp
assignees: []
milestone: mvp
parent: TKT-01M3558D9Z1ZWHXB3V14A4AJGR
origin: null
dependencies:
  - TKT-01M3558DB2BJEEYN3G8M5GAJ17
  - TKT-01M3558DBS1J4V1VB5C0FVNWKP
  - TKT-01M3558DCDJ71TN19N5DDY4RSF
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:11Z
updated_at: 2026-09-22T20:21:17Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: human:sothr
  name: Drew Short
extensions: {}
---

## Description

Promote scaffold `sync` from bare CAS upload to full pipeline (redact, watermark, manifest ACK). Re-sync must upload zero new blobs for unchanged files.

## Acceptance criteria

- [x] sync runs redact, watermark, and manifest ACK on the same path as the agent
- [x] Re-sync of an unchanged file uploads zero new blobs

## Implementation plan

upload.Sync is the shared path. For each terva session it allowlists, scans with ruleset v1, asks watermark.Plan, enqueues the digest and manifest in the outbox, PUTs only digests the lake does not have, POSTs the manifest, and on ACK commits the watermark and acks the outbox.

An unchanged file whose digest is already in the CAS produces no PUT. A grown file is still one whole blob: the lake does not assemble tails yet. Plan is what suppresses the unchanged upload. The long-running agent loop stays a later ticket and will call this same Sync.

## Summary

upload.Sync is the shared path: allowlist, ruleset v1, watermark.Plan, outbox enqueue, blob check and PUT of missing digests, manifest POST, then watermark.Commit and outbox Ack. A failed manifest POST leaves the cursor where it was and leaves the outbox row pending.

Re-sync of an unchanged file checks the digest and PUTs nothing. A grown file is still one whole blob, because the lake does not assemble tails; Plan is what suppresses the unchanged upload. The long-running agent loop is still TKT-01M3558DDS1NVWKCC2S36TYPZV (Long-running terva-lampi agent daemon loop). It will call this Sync rather than a second pipeline.
