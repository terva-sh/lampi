---
schema: 3
id: TKT-01M3SZQ69BM4CV7R6NDQNY5B9R
title: "Transcript page: collapse runs of unknown harness events"
type: task
status: draft
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
updated_at: 2026-09-30T20:22:27Z
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

- [ ] Three or more consecutive unknown events without text render as one collapsed <details> naming the range, the count and each raw type
- [ ] Expanding a run shows the original cards, with #e-N ids, links and excerpt selection unchanged
- [ ] An unknown event with text ends a run and stays visible
- [ ] A run holding the page's target event renders open, and an open run stays open across the live refresh
- [ ] Each unknown card shows its raw type
- [ ] internal/web/transcript_test.go covers a run, a short stretch, a run with text inside it, and a target inside a run
