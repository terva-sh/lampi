---
schema: 3
id: TKT-01M44B45MT89CGR6M0HHTWG63P
title: "Cursor CLI: upload store.db blobs as content-addressed objects"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/adapter
  - area/protocol
  - area/cas
  - area/normalize
  - phase/4-cursor
assignees: []
milestone: null
parent: null
origin: null
dependencies:
  - TKT-01M44B45JRVQ1W2XF5H0K3MC4X
  - TKT-01M44DDXWQ6MG51BMVF5FDCAEA
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-04T20:54:11Z
updated_at: 2026-10-04T22:19:56Z
created_by:
  id: agent:claude-code/580cbe08
  name: ""
updated_by:
  id: agent:claude-code/580cbe08
  name: ""
extensions: {}
---

## Description

A Cursor CLI session is uploaded as one JSON document that holds every row of `store.db`. The document is rebuilt and uploaded whole on every change: the artifact's `byte_watermark_prev` is always 0. Blobs are written in id order, and a blob id is a content hash, so a new blob lands at a random place in the document and shifts every fixed 4 MiB upload piece after it. A change costs close to the whole session in upload and in lake storage.

ACP sessions on one workstation run to 744M and 2.5G. At the agent's 30-second debounce cap, one active session would re-upload hundreds of megabytes a minute and store each version.

A blob id is the hex SHA-256 of the blob bytes (2,000 of 2,000 checked on a live ACP store). So the blobs can go to the lake as their own content-addressed objects and the session document can list them by id, so a change uploads only the new blobs and the stored versions share the old ones. This changes what the lake stores for `cursor-cli` and what the normalize projector reads, so it needs a design that the lake, an older agent and an older lake can all live with.

## Acceptance criteria

- [ ] A change to a Cursor CLI session uploads only blobs the lake does not already hold
- [x] The lake can still read, normalize and export sessions stored in the whole-document form
- [x] An older lake, or a newer lake with an older agent, keeps working
- [x] The redaction scan still covers every blob that is uploaded

## Implementation plan

Use the lake's existing chunk lists rather than a new protocol. A file over the lake's object cap already goes up as chunks: the client asks which chunk digests the lake lacks, puts those, and posts `chunk_sha256s` and `chunk_lengths`; the lake binds a logical file and stores no whole copy. The chunks were fixed 4 MiB slices, so one inserted blob shifted every chunk after it.

- `cursorcli.encodeDocument` writes the export one blob row at a time. The bytes are exactly `json.Marshal(doc)`, so existing heads keep their digests. It also returns chunk lengths: a chunk ends after a row whose id's SHA-256 has a first byte under 256/32, or before a row that would take it past 4 MiB; a longer row is cut into 4 MiB pieces.
- `adapter.Bundle.Cuts` carries the lengths by digest, and `Load` fills them in after a memo hit. `prepared.cuts` takes them to `uploadDigests`. `uploadSplit` uses them when they cover the body in pieces that fit, and falls back to fixed pieces otherwise.

Alternatives considered:

- Upload each blob as its own CAS object, with a new artifact kind listing blob ids. Lost: a protocol change, a second document form for normalize and export, and a lake and agent version matrix to keep working, for the same saving the chunk list gives.
- One chunk per blob row. Lost: 20,000 digests per manifest and per blob check for one session, against about 600 with grouped rows.
- Fixed-size content-defined chunking (a rolling hash over bytes). Lost: rows are already content-addressed and sorted, so cutting at chosen rows is deterministic without a rolling hash, and keeps a row whole.
- Cut exports under the 32 MiB cap as well. Left out: the lake installs a short chunk list as one whole object anyway, so storage does not shrink, and the chunks are kept beside it.

## Notes

**agent:claude-code/580cbe08** at 2026-10-04T21:40:21Z

Measured end to end on a copy of a real 222 MB ACP store (284,062,438-byte export) against a test lake: the first sync put 435 chunks (284 MB); after inserting one blob into the store, the next sync checked 435 and put 1 chunk, 3,032,178 bytes. The lake read the 284 MB logical file back with a matching digest. The run then sat in the lake's normalize worker for 9 minutes, which is TKT-01M44DDXWQ6MG51BMVF5FDCAEA (Cursor CLI normalize searches the whole export once per row), now a dependency. In unit tests, one added blob whose id sorts first leaves at most 2 of the chunks new, where a fixed split of the same two exports shares under half; the encoding equals json.Marshal; the upload half re-sends only the changed chunk against a real test lake; prepare carries the cuts for a built export and for one Load builds after a memo hit.

**agent:claude-code/580cbe08** at 2026-10-04T21:40:21Z

Acceptance criterion 1 is left unticked on purpose. A change uploads only the chunks the lake lacks when the export is over the lake's 32 MiB object cap; an export under it still uploads whole, once per settled change (see the plan for why). And the unit is a chunk of about 32 rows, not one blob. Correction to TKT-01M44B45JRVQ1W2XF5H0K3MC4X: its plan and note say this ticket removes the reason for the 256 MiB store cap. It does not: the export is still built whole in memory, and the upload still reads it whole. Streaming the export is filed as a draft follow-up.

**agent:claude-code/580cbe08** at 2026-10-04T22:12:11Z

terva-review on #196 (review 2132, medium): uploadSplit built the fixed-size chunks and then the reader's, copying the export twice. It now picks one strategy first, and splitAt's chunks share the body's bytes instead of copying (the body is not written to and outlives the upload). Measured: splitting an 8 MiB body with reader cuts now allocates under 1 MiB.

## Summary

Landed in #196 (merge b64320f), with no protocol change. The Cursor CLI export is written one blob row at a time (byte for byte json.Marshal, so stored heads keep their digests) and cut after rows picked by id hash, about 32 rows and at most 4 MiB a chunk. Bundle.Cuts carries those lengths to uploadSplit, which uses them for an export over the lake's 32 MiB object cap; the lake binds the chunk list as a logical file, as before. Unchanged chunks keep their digests and are not sent. Real data: on a 284 MB export, adding one blob re-sent 1 chunk of 435 (3 MB), and the lake read the file back with a matching digest. Lake normalize of that export takes 2.4 s after TKT-01M44DDXWQ6MG51BMVF5FDCAEA. terva-review's one finding (the export copied twice when splitting) was fixed. Criterion 1 is not fully met and stays unticked: an export under 32 MiB still uploads whole, and the unit is a chunk of rows, not a blob. The export is still built in memory, so the 256 MiB store cap from TKT-01M44B45JRVQ1W2XF5H0K3MC4X stays; streaming is TKT-01M44DRPSD0FHC969KWV5B9YZV (draft).
