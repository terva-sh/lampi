---
schema: 3
id: TKT-01M3FPP3H592T31Y2M3N347CPB
title: "Session recall: one query surface for the web UI and an MCP server"
type: epic
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
  - area/server
  - area/auth
  - area/docs
assignees: []
milestone: null
parent: null
origin: null
dependencies:
  - TKT-01M3F2PGDY06D7XE12NWQ9EZF4
  - TKT-01M3F2PGHM6VHQBNE5XKDXS407
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-26T20:32:10Z
updated_at: 2026-09-26T21:14:01Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

### Decisions

These were settled with the owner on 2026-09-26. Every child ticket and every release B ticket that touches search or reads events takes them as fixed input.

1. **The web UI and MCP use one surface.** Search, structured filters, event paging, deep links and copy-out live in one server-side query layer. The browser JSON API and the MCP server are thin adapters over it, with the same parameters, cursors, limits and result shapes. A capability goes into the query layer first and is exposed through both adapters. It is not built inside one and ported later.
2. **MCP is for recall by agents on the owner's machines.** An agent asks the lake about past work across every machine that has uploaded.
3. **This replaces `terva-ext-session-search`.** That extension covers one project on one machine. The lake covers every project on every machine. Build for the replacement case, not alongside it.
4. **Auth builds on the existing OIDC identity.** Browser users already authenticate with OIDC. MCP clients will authenticate either by extending OIDC to the MCP client or with bearer tokens that a user generates in the UI and that act as that user. The choice is deferred to the MCP auth child ticket. Whichever wins, the token maps to an OIDC identity and its roles, is revocable, and is never a device token. Device tokens keep their meaning: they authorize `/v1` ingestion only.
5. **Prompt injection is out of scope for the first pass.** This is an owner-controlled system. Transcript text goes to MCP clients as stored. Revisit this before any non-owner or shared deployment.
6. **Structured search is a first-class query, not an add-on to text search.** Filters on `event_type`, `actor`, tool name and `raw_type` compose with literal text, session filters (harness, project, machine) and the UTC time range. Example questions: "sessions that ran `git push`", "failed tool calls in project X".
7. **Every hit and event has a deep link.** An address is session UID + generation + event position. The web viewer opens it scrolled to the event, and MCP results carry the same link, so an agent can hand a human a URL. A link to a superseded generation says so; it does not silently show different content.
8. **Copy-out is a first-class action.** A selected span of events can be copied out in a form ready to paste into a new agent session: from the viewer through the browser, and from MCP as a tool result. Whether this goes through release B's `exporter` role and `export_projects` gate, or has its own smaller gate, is decided in its child ticket, and the decision is recorded there.

### Result shape

A search hit carries the session UID, harness, project, machine, generation, event position, recorded time, a snippet and the deep link. The shared layer can fetch a window of events around any position (for example N-5..N+20) with the same generation-safe cursor that the release B viewer uses. Agents need this window to read context around a hit, and humans get the same thing as "jump to hit".

### Relationship to release B

Release B (TKT-01M3F2PGA, Web retrieval: browse, search and export stored sessions) plans the viewer (TKT-01M3F2PGD), the FTS5 index (TKT-01M3F2PGH), the search UI (TKT-01M3F2PGM) and exports (TKT-01M3F2PGR). Decision 1 changes how they are built: their query functions belong in the shared layer, and PGM's browser API is one of its adapters. Neither the index design nor the release B contract otherwise changes. This epic adds structured filters, deep links, copy-out, MCP transport and MCP auth.

### Future direction, out of scope

Agents that keep no local transcript at all: the harness streams the session straight into lampi, so an ephemeral agent leaves nothing on its host. That changes the ingest protocol and the sync model, not only retrieval. File it separately when it becomes concrete. Nothing in this epic should rule it out. In particular, recall must not assume a session also exists on a local disk.

### Children to file

This epic is filed without children. Likely splits:
- shared query layer extraction and result shape
- structured filters
- deep links and viewer anchoring
- copy-out
- MCP server transport and tools
- MCP auth (OIDC for MCP clients or user-generated bearer tokens)

## Acceptance criteria

- [ ] Web and MCP adapters call the same query functions and return the same result shapes, cursors and limits.
- [ ] Structured filters (event_type, actor, tool name, raw_type) compose with literal text, session filters and time range in both adapters.
- [x] Hits and events carry a generation-pinned deep link that opens the event in the viewer and reports a superseded generation.
- [ ] A selected event span can be copied out from the viewer and from MCP in a paste-ready form, under a recorded authorization decision.
- [ ] An MCP client authenticates as an OIDC-backed user identity with revocable credentials; device tokens do not authorize it.
- [ ] Documentation states that this replaces terva-ext-session-search and how an agent is configured to use it.

## Definition of done

- [x] Children filed and linked, decisions reflected in docs/web-ui-plan.md, evidence recorded in the ticket.

## Notes

**agent:claude-code/cd41c9ac** at 2026-09-26T21:14:01Z

### Progress on 2026-09-26
The web side of recall is implemented on branch t3code/explore-store-ui-search. internal/recall is the shared layer:
- Reader.Events: generation-pinned event pages.
- Index.Search: literal and structured search.
- EventLink: deep links.
- Reader.Excerpt: copy-out.

Web adapters: /api/web/v1/sessions/{uid}/events, /search, /sessions/{uid}/excerpt, and the transcript, search and plain-text pages. Children TKT-01M3FPWC9E, TKT-01M3FPWCBK and TKT-01M3FPWCDF are done, as are the release B tickets TKT-01M3F2PGD, TKT-01M3F2PGH and TKT-01M3F2PGM they build on. The decisions are now in docs/web-ui-plan.md under "Recall surface".

### Criteria status
- Ticked: AC3 (deep links) and the definition of done.
- Open: AC1, AC2 and AC4 each need the MCP adapter (TKT-01M3FPWCFS, MCP: serve recall tools over the shared query layer) to exist. AC5 needs the auth decision in TKT-01M3FPWCH4 (MCP: authenticate clients as OIDC users). AC6 is the MCP configuration doc.

Both MCP tickets are draft. Promote TKT-01M3FPWCH4 after choosing between OIDC for MCP clients and user-generated bearer tokens.

### Interfaces the MCP adapter will reuse
- recall.Reader.Events / EventRequest
- recall.Index.Search / SearchRequest (with EventTypes and Actors)
- recall.Reader.Excerpt / ExcerptRequest (set Origin to the lake base URL)
- recall.EventLink

cli.startWeb shows how the index is started and stopped (OnPublished, BeforeClose). An MCP server needs the same wiring whether or not web is enabled.
