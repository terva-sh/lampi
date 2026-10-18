# Incremental projection decision

The memory epic ships bounded JSONL projection and admission/coalescing. It
retains complete projection after a changed artifact. Durable append projection
and search prefix skipping are deferred to the two follow-ups below, because
reusing an immutable event prefix would change current observable semantics.
This is the decision from TKT-01M56GVA966MW31426MC7552PC (CPU: evaluate
incremental projection and search prefix reuse).

## What must be preserved

`Claude`, `Codex` and `Terva` emit random event IDs using the projection time.
`ingested_at` is that projection's time; `content_ref` identifies the current
full artifact digest and the source byte offset. An append changes the full
artifact digest, even for earlier records. A reused prefix would preserve old
IDs/timestamps/references where full projection produces new ones. Assigning
deterministic IDs or rewriting reference resolution would need an explicit
semantic version and migration decision. The memory changes make neither.

The parsers also carry state across lines. Claude retains session, cwd, model
and branch; Codex adds provider and commit; terva carries id, cwd, parent, model,
provider and version. Constructor options include manifest project/lineage/git
metadata, harness version, artifact digest and fallback projection time. A
checkpoint must version and cover every value that can affect subsequent
emission, not just the last byte offset. Malformed-line counts, first-error
metadata and whether any valid objects were seen affect whole-input failure.

## Proposed checkpoint protocol

1. Identify each selected artifact by ordered manifest position, kind, relpath,
   content/chunk digests, normalizer version and constructor options. Include
   the full selected-artifact/sidecar list in the session proof. A duplicate
   relpath or newly selected sidecar must follow the same head-selection rules
   as a full pass.
2. Record the last complete record boundary, source line/byte offsets, emitted
   event count, parser/error state and authenticated prefix identity. A size or
   mtime match is insufficient: verify the prefix against trusted immutable CAS
   content/chunks. Record the logical offset independently of chunk boundaries.
   Verification may still read O(input) bytes and must be measured separately.
3. Never checkpoint past a partial final record. Today's parser can emit from a
   final valid JSON record without a newline; an append can extend that same
   record. Roll back its events and parser state to the preceding complete
   boundary, then replay the tail. Invalid partial data can change error counts
   or whole-input success, requiring a complete failure decision before publish.
4. On rewrite, truncate/regrow, divergent head, changed prefix, reordered or
   replaced sidecar, metadata/options/version change, or missing/corrupt proof,
   fall back to full projection. A changed earlier sidecar invalidates dependent
   later output. Do not infer append from a larger file or a matching tail.
5. Write a private complete candidate generation, then recheck session gen/head
   under the publication lock. Commit checkpoint identity only with the matching
   published output. Superseded candidates are discarded. A crash before catalog
   publication must leave a durable job that rebuilds; a checkpoint must never
   describe files that were not fully published. Recovery ignores uncommitted
   checkpoint/output files and removes abandoned private stages under lake.lock.
6. Resolve prefix identity semantics before enabling reuse. Keeping old immutable
   references requires proving those CAS objects survive compaction/collection;
   rewriting references to the new full digest means reserializing every event.
   A versioned source-reference indirection or generation manifest is possible,
   but adds read/backup/purge/GC obligations. Immutable prefix and tail segments
   also need bounded file counts, compaction and date-partition handling. The
   current per-file publication mechanism must not be mistaken for an atomic
   directory-generation transaction.

An in-memory offset cache was rejected: restart loses its proof and it cannot
safely handle same-size rewrites, partial records or parser-state changes.
Skipping straight to an uploaded chunk boundary was rejected because JSON
records and state cross chunk boundaries. Reusing prefixes without changing
references was rejected as an implicit identity/freshness change. Rewriting the
prefix preserves current references but keeps O(input) serialization/compression,
so it cannot establish an O(append) processing claim.

## Search prefix reuse

Search already compares signatures of searchable fields and writes only changed
rows in one SQLite transaction. It still loads old row signatures, decompresses
and decodes the new event stream. Random event IDs alone do not prove searchable
content changed or stayed unchanged; `docRow.sig` defines the relevant fields.

A safe optimization can store a generation-bound fingerprint per normalized
frame over every searchable field, order, harness/project metadata and search
semantic version. An identical prefix proof can retain FTS rows and skip both
old-signature loading and prefix event decoding. Missing proof uses today's
complete pass. Rewrites/truncation/sidecar and metadata changes invalidate the
proof; publication gen/head must be checked again before the index commits.
A superseded pass must not mark the new head indexed. Crash recovery compares
catalog publication and index generation, as it does now. Proof creation itself
has CPU/storage cost and needs measurement; hashing compressed frames is not
sufficient when harmless event IDs/timestamps change their bytes.

## Follow-ups and performance targets

- TKT-01M56M4CD83MDCQ14FGHB7MNBR — Normalize: define durable append identity and
  checkpoint contract. Decide versioned identity/reference lifetime, then target
  at least 50% lower normalize CPU for a 64 KiB append to 128 MiB, preserving the
  bounded working set and measuring prefix verification separately.
- TKT-01M56M4CEXFZ0T0JME4959T7G1 — Search: prove unchanged prefixes before skipping
  event decoding. Target at least 50% lower search-pass CPU for a 64 KiB append
  to 32 MiB with identical query results/highlights/pagination and index growth.

Both are draft design/prototype work beyond this release's evaluated scope.
Their regression cases include full-vs-incremental append, malformed/partial
records, rewrite/truncate/regrow, changed parser state, sidecars, project/lineage
metadata, version invalidation, references after compaction, restart and
supersession. The component/search measurements in [performance.md](performance.md)
quantify the opportunity and their limits. Debounce can reduce repeated jobs
before these designs ship, but adds visibility delay and is an explicit operator
tradeoff; no capture policy or debounce default changed.
