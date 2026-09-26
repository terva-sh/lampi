---
schema: 3
id: TKT-01M3FPWCDFXXCHD8F5PA0GGMWP
title: "Recall: copy a selected event span out in paste-ready form"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
assignees: []
milestone: null
parent: TKT-01M3FPP3H592T31Y2M3N347CPB
origin: null
dependencies:
  - TKT-01M3F2PGDY06D7XE12NWQ9EZF4
blocks_on: none
references: []
claim:
  actor: agent:claude-code/cd41c9ac
  branch: t3code/explore-store-ui-search
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-946c2db7
  commit: e99d2b9d578a335ad3f714f54dc3119eee787ede
  session: null
  claimed_at: 2026-09-26T21:09:13Z
  expires_at: null
archive: null
created_at: 2026-09-26T20:35:35Z
updated_at: 2026-09-26T21:13:15Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

Let a viewer select a contiguous span of events in one session and copy it as plain text ready to paste into a new agent session, with a header naming the session, harness, project and deep link. The same span rendering is a shared recall function so the MCP adapter can return it. Span length and output bytes are bounded. Decision 8 of the recall epic applies. Authorization decision: copy-out is a viewer action because it returns only text the viewer role can already read on screen; bulk downloads and training formats stay behind release B's exporter role and export_projects policy.

## Acceptance criteria

- [x] A viewer can select a bounded span in the transcript and copy it; without JavaScript the same text is available as a plain-text page.
- [x] Rendering is shared, bounded by events and bytes, keeps order, marks truncation and never decrypts opaque content.
- [x] Tests cover span validation, bounds, stale generations and malicious content.

## Implementation plan

recall.Reader.Excerpt renders a span [from, from+count) of one pinned generation as plain text. A header names the harness, session, project, span and an absolute link to the first event (web passes its base_url as Origin). Each event follows as a [#pos actor type tool (error) time] line with its text. Bounds: 200 events, 64 KiB per event with a marked cut, and 512 KiB in total, after which the span stops early and says so. Encrypted extra values become an 'omitted' line. The web layer serves it as JSON at /api/web/v1/sessions/{uid}/excerpt and as a text/plain page at /sessions/{uid}/excerpt. On a transcript page, JavaScript adds per-event checkboxes with shift-click ranges and a sticky bar with Copy as text (clipboard) and Open as text. Without JavaScript, each page links its own plain text.

## Notes

**agent:claude-code/cd41c9ac** at 2026-09-26T21:13:14Z

### Decisions
- **Authorization: copy-out needs only the viewer role.** This was decided when the ticket was filed and is kept here. The excerpt returns only text the viewer role already reads on the transcript page. Bulk downloads and training formats stay behind release B's exporter role and the export_projects policy (TKT-01M3F2PGR).
- **Labelled plain text, not JSON lines.** The purpose is pasting into a new agent session, where a readable transcript with positions and a source link is more useful than raw events. The events API remains the structured form.
- **The plain page is text/plain with nosniff.** It needs no escaping and cannot render as HTML whatever the transcript holds. The test checks that the literal `<script>` survives in the text and that the content type is plain.
- **Selection stays within one page (at most 200 events, matching the excerpt cap).** Cross-page selection was left out. The API takes any from and count, so MCP is not limited by it.
- **Clipboard failures fall back to Open as text.** The script tracks whether the fetch succeeded, rather than parsing error messages.

### Evidence
- recall TestExcerptRendersBoundedPlainText covers span, order, header, the tool error flag, literal malicious text, opaque omission, the per-event cut, the byte cap, validation and a stale generation.
- web TestExcerptAPIAndPlainPage covers the guard, JSON and plain text returning identical text, content type and nosniff, bad inputs on both routes, a stale generation, and the page controls.
- The browser smoke selects #2, shift-clicks #6, copies, reads the clipboard, compares it with Open as text, and checks the no-JS plain link.
- `just ci` passes; race runs of recall and web pass.
