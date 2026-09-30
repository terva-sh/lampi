---
schema: 3
id: TKT-01M3SZQ69BM4CV7R6NDQNY5B9R
title: "Transcript page: collapse runs of unknown harness events"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-30T20:22:27Z
updated_at: 2026-09-30T23:07:43Z
created_by:
  id: agent:claude-code/27b21f4b
  name: ""
updated_by:
  id: agent:claude-code/27b21f4b
  name: ""
extensions: {}
---

## Description

On the transcript page, a Claude Code session can show long stretches of `Harness` / `unknown` cards with no body, which push the conversation apart. The owner asked on 2026-09-30 for such a stretch to collapse into one expandable section. The screenshot that prompted this showed #3 to #15 of a talkoot session, 13 cards in a row.

### Where they come from

- `internal/normalize/claude.go:111-120` routes every record type the projector does not name to `unknownLine` (:375-388), which emits `EventUnknown` with no text. This covers `file-history-snapshot`, `queue-operation`, `attachment`, `progress`, `custom-title` and others.
- `systemLine` (:313-344) does the same for every `system` subtype except compaction and errors.
- `actorFor` (`terva.go:444-460`) gives each of these the `harness` actor.
- The record's own type is kept in `Event.RawType` (`event.go:48`), but the page never shows it, so every card reads just "unknown".

### Approach

This is server-side in `transcriptPage` (`internal/web/pages.go:340-388`) and the `transcript` template (`internal/web/templates/page.html:49-58`), with no JavaScript, as `lake.js:1` requires.

- Group the page's items into runs. A run is **three or more consecutive** events whose type is `unknown` and that carry no content text. A shorter stretch renders as it does today.
- Render a run as a `<details>` element inside the `<ol>`. Its `<summary>` names the range and what the run holds, for example `13 unknown harness events, #3–#15: file-history-snapshot ×9, queue-operation ×4`. Its body holds the original `<li>` cards unchanged, so `#e-N` ids, links and the excerpt checkboxes keep working.
- An `unknown` event with text, such as the `ai-title`/`summary` line that shows "Talkoot avatar notes", ends a run and stays visible.
- If the page's target event (`?at=N` or the `target` class) falls inside a run, render that run `open`. Give each `<details>` an id so `lake.js:39` keeps it open across the live refresh.
- Runs are grouped per page. A stretch that crosses a page boundary collapses separately on each page. Page size and positions don't change.
- Show `RawType` beside `unknown` on every card, collapsed or not, so an expanded card says what it is.

### Out of scope, noted for follow-up

- **Title records render as "unknown".** `titleLine` (`claude.go:346-373`) puts the `summary`/`ai-title` text on an `EventUnknown`. A title event type would be a normalize schema change.
- **Missing timestamps.** A record with no `timestamp` gets its normalization time as `recorded_at` (`terva.go:472-477`, asserted at `claude_test.go:193`). After a renormalize, such cards show a date like 2026-09-30T20:18:11 inside a 09-28 session. The page could mark these as having no recorded time.

## Acceptance criteria

- [x] Three or more consecutive unknown events without text render as one collapsed <details> naming the range, the count and each raw type
- [x] Expanding a run shows the original cards, with #e-N ids, links and excerpt selection unchanged
- [x] An unknown event with text ends a run and stays visible
- [ ] A run holding the page's target event renders open, and an open run stays open across the live refresh
- [x] Each unknown card shows its raw type
- [x] internal/web/transcript_test.go covers a run, a short stretch, a run with text inside it, and a target inside a run

## Notes

**agent:claude-code/27b21f4b** at 2026-09-30T23:07:43Z

### Built, 2026-09-30

- **`internal/web/transcript_runs.go`**: `transcriptBlocks` groups a page's items. An event is *quiet* when it is `unknown`, has no content text, and is not oversized, unreadable, opaque or truncated, so no hint is lost. `minQuietRun = 3`. `unknownKind` names the raw type, with `/subtype` or `/block_type` from `Extra` when present, e.g. `system/local_command`.
- **Template**: the card moved into `{{define "event"}}`. A run renders as `<li class="event-run"><details id="run-FROM">`, whose summary reads `N unknown events #FROM–#TO kinds`, wrapping a nested `<ol class="events">` of the unchanged cards. Every unknown card shows its kind in a chip beside `unknown`.
- **Target**: `transcriptPage` computes the blocks with the deep link's target, and a run holding it renders `open`. `lake.js` unfolds the enclosing `<details>` before focusing a bare `#e-N` target.
- **Test**: `TestTranscriptFoldsRunsOfQuietUnknownEvents` covers a run of three with two kinds, a pair that stays as cards, a pair cut short by a titled `ai-title` event, which stays visible, a `system/local_command` run, cards nested inside their run, and `?at=12` opening run 11 and marking e-12. Setting `minQuietRun` to 1000 makes it fail.
- **Checked in a browser**: headless Chromium rendered the smoketest transcript, both light folded and dark opened by `?at=11`. The run reads as one dashed row. Opened, it shows four cards with checkboxes and kind chips, and the target is highlighted.
- `GOFLAGS=-mod=mod just ci` passes.

Criterion 4 is half verified. Server-side `open` for the target is tested. Staying open across Refresh relies on the existing `details[id][open]` handling in `lake.js:39` and the stable `run-FROM` id, but no browser run exercised it. To check it by hand: open a run, press Refresh now, and the run should stay open.

The smoketest's preview browser could not reach loopback, so screenshots came from `chrome-headless-shell` behind a local proxy that added the synthetic session cookie.

## Summary

Runs of three or more quiet unknown events on the transcript page fold into one <details> that names the range and each raw type. Cards inside are unchanged. A deep link into a run opens it, and every unknown card shows its raw type. Refresh keeping a run open is unverified in a browser; see the note.
