---
schema: 3
id: TKT-01M3NQ2RT5G1TEY54R9R8RXG2G
title: "Normalize: a torn line mid-file fails the whole session"
type: bug
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/normalize
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/27b21f4b
  branch: normalize/torn-line
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-27b21f4b
  commit: 57f2200b1301809bd8ac52626eeef238c76d4b2e
  session: null
  claimed_at: 2026-10-01T03:35:04Z
  expires_at: null
archive: null
created_at: 2026-09-29T04:34:31Z
updated_at: 2026-10-01T04:42:12Z
created_by:
  id: agent:claude-code/16ebd168
  name: ""
updated_by:
  id: agent:claude-code/27b21f4b
  name: ""
extensions: {}
---

## Description

Two sessions on the internal lake fail to normalize with `normalize: line N is not a JSON object` (seen 2026-09-29 after the v0.3.0 deploy):

- `01M3NK7HRF1KNKVJ0QPS5WG726`, line 139, first seen 03:27Z. The file is not on the workstation, so it came from another device.
- `01M3NNF6B0VSE95KK0V8YX6758`, line 691, first seen 04:06Z. It is a Claude Code transcript in a `terva-sh/tuohi` worktree, uploaded when the workstation's allow rules widened (TKT-01M3NMHDWR).

### Cause

The file is torn, and lampi did not tear it. Line 691 of the tuohi transcript is a `queue-operation` record cut off mid-string, with the next record (`{"type":…`) written on the same line and no newline between them. The lines before and after it parse. Claude Code wrote it that way at 2026-09-28 19:31Z.

### Why the whole session fails

TKT-01M38RJT92 (Claude Code normalize projector) specified that "a line that is not a JSON object fails the blob". The Codex projector does the same. The capture adapters already skip such lines when reading identity (`internal/adapter/claude/claude.go` readIdentity). The raw bytes are kept intact in the CAS, so nothing is lost. But one torn line leaves the whole session with no events, search entries or transcript view.

### Proposal

A line in the middle of a file that is not a JSON object becomes a marker event, and the session keeps going. The marker carries the line number and byte offset, but not the line's content, keeping the rule that the error text does not include the line. The session could be flagged as normalized with warnings. A torn *last* line in a file still being written is a different case, and waiting for the next sync may be right there.

Decide first whether the strict rule still serves a purpose. It may exist to catch a projector bug early rather than paper over it, and a marker event would need to keep that visible.

## Implementation plan

### Decision

The strict rule does not catch projector bugs. It rejects input the harness wrote, and lampi's projectors never write raw lines. Its cost was the whole session: both failures on the internal lake (01M3NK7HRF line 139 and 01M3NNF6B0 line 691) left complete transcripts with no events, search entries or transcript view, because of one line Claude Code tore.

Alternatives considered:
- **Keep strict, and expose the failure better.** Lost because the session stays unusable, and the cause is outside lampi's control.
- **Skip the line silently.** Lost because nothing shows the gap, which is what the strict rule rightly cared about.
- **Salvage the complete record joined onto a torn one.** Postponed. The torn line 691 is a cut-off record with the next whole record on the same line. Recovering that record means guessing where the record starts, and a wrong guess emits an event the harness never wrote. It is worth a separate ticket if lost records turn out to matter.
- **Marker event (chosen).** An `error` event at the line's position names the line number and its byte length, and never its bytes, keeping the rule that the error does not quote the line. The transcript page shows it as a visible card, since it has text, and search can find it.

A file in which no line is a JSON object still fails with the first line's error, so a file of the wrong format is reported rather than turned into a session of markers.

### Change

- `internal/normalize/unreadable.go`: `unreadableLine` error, marker text and extra (`unreadable_line`, `line_bytes`).
- In Claude, Codex and terva, `line()` returns `unreadableLine`, and `Normalize` emits the marker through its own `emit`, counts object lines, and fails only when there are none.
- `docs/architecture.md` describes the rule.

OpenCode, Cursor and Cursor CLI take one JSON document per blob, so they are unaffected.

## Notes

**agent:claude-code/27b21f4b** at 2026-10-01T04:42:12Z

### Verified, 2026-09-30

- **Reproduced first.** A scratch lake on the v0.5.0 binary received, from a scratch agent, a Claude transcript whose line 3 is a cut-off record with the next record joined on (the shape of line 691). It failed with `normalize: line 3 is not a JSON object`.
- **Diagnosis tool.** A throwaway read-only tool (`lampi-linediag`, built in a scratch worktree, not committed) read the stored blob through `cas.Store.Read` and reported `bad line 3/4: len=237 … whole_object_suffix_at=40`, without printing content.
- **Fixed.** A build of this branch took `serve normalize --failed` on that lake, and the session went to ready. Its events are: message, message, `error` (harness) "Line 3 of the raw file is not a JSON record and was skipped (237 bytes)…" with `unreadable_line=3`, and message. Neither the cut-off text nor the joined record's text appears in the events file. The joined record is lost with the line; see the plan.
- `GOFLAGS=-mod=mod just ci` passes.

**On the internal lake**, after the release with this fix is deployed, `serve normalize --failed` and a SIGHUP should bring both sessions to ready.
