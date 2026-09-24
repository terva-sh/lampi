---
schema: 3
id: TKT-01M3558E21PZDJEYAAR4WH9B85
title: Soft-link git-ticket claims to lake session_uid
type: spike
status: done
status_reason: null
priority: low
due_on: null
labels:
  - area/ops
  - phase/3-export
assignees: []
milestone: phase-3
parent: TKT-01M3558DZ6CBW2HB49WFFY20CP
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:12Z
updated_at: 2026-09-24T03:34:48Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/d336
  name: Cursor cloud agent
extensions: {}
---

## Description

Whether a git-ticket claim that names a terva session should resolve to the lake `session_uid`.

### The two id spaces

A claim records an actor. For terva that actor looks like `agent:terva/session-…`. Schema 3 also stores `claim.session` as free text. git-ticket does not parse it. The field exists so a ticket can point at the transcript that did the work, and only the harness knows the id's shape.

The lake assigns `session_uid` once per `(harness, native_session_id)`. An alias maps `(harness, native_id, machine_id)` to that uid. A second machine posting the same native id joins the same uid and adds provenance rather than a new blob. Normalized events identify the session as `terva:` plus the native id, which is not the uid. Layer B is in docs/architecture.md. The manifest ACK rules are in docs/protocol.md.

Nothing in lampi reads a claim. The actor suffix and `claim.session` are not alias keys, and they are not `session_uid`.

### Why it matters

lampi is the session lake for terva ops. Claims say which agent session holds a ticket. The lake is where that session's bytes, catalog row, normalized JSONL (`normalized/<session_uid>.jsonl`), parquet, and divergent copies live. A soft link would be a read-time lookup from the claim to that uid. It would not store `session_uid` on the claim, and it would not store a ticket id in the catalog.

Phase 3 (TKT-01M3558DZ6CBW2HB49WFFY20CP, Phase 3 export-based harnesses & conflicts) listed this as optional git-ticket to session_uid glue and left it parked. Export, sidecars, and the conflict list did not need the join. The open question is whether a claim already carries enough to find the lake row, or the two id spaces should stay separate.

### Decision the spike must record

Record exactly one outcome.

- Wire the soft link. Name the lookup key (`claim.session`, the `session-…` suffix of an `agent:terva/…` actor, or both), the harness, and whether the key matches `native_session_id` or `session_uid`. An unknown key stays unresolved and does not create a session. Say where the rule will be written. Shipping the lookup is a follow-up ticket, not this spike.
- Do not wire it. Claims stay opaque strings. `session_uid` stays an identity the lake assigns at ingest. Say why joining them would be wrong.
- Defer, with criteria. Name the operator question that is still unanswerable from a claim plus the catalog, and the evidence that would open an implementation ticket later.

### Out of scope

A catalog migration, a change to the claim format, and a production resolver. Reopening the Phase 3 epic. This spike ends when the decision is recorded in the summary or in a short architecture note the summary cites.

## Acceptance criteria

- [x] Record how an agent:terva/session-… actor and schema-3 claim.session relate to (harness, native_session_id) and to session_uid, including when they do not match.
- [x] Record exactly one decision: wire a soft link, do not wire one, or defer.
- [x] When the decision is wire, name the lookup key, the harness, whether the key matches native_session_id or session_uid, and that an unresolved claim does not create a lake session.
- [x] When the decision is do not wire or defer, state why. A defer also states the criteria that would open a later implementation ticket.
- [x] Put the decision in the ticket summary, or in docs/architecture.md with the summary pointing at that paragraph.

## Definition of done

- [x] The decision is recorded in the summary, or in an architecture note the summary cites.
- [x] This spike does not ship a catalog schema change, a claim-format change, or a production resolver. If the decision is to wire, the implementation is a separate draft ticket.
- [x] git ticket check passes after that record lands.

## Implementation plan

### Where the decision goes

Record one decision, do not wire, in docs/architecture.md in the paragraph that follows the Layer B description. The ticket summary cites that paragraph. No catalog migration, no claim-format change, and no resolver.

### What the record has to cover

State how an agent:terva/session-… actor and schema-3 claim.session relate to (harness, native_session_id) and to session_uid, including the cases that do not match. State why a soft link would be the wrong join. Leave the Phase 3 parent epic done.

## Notes

**agent:cursor/c937** at 2026-09-23T17:30:24Z

Phase 3 promote left this parked. The description says to park until the Phase 3 need is clear, and this promote did not establish that need. It stays draft.

**agent:cursor/cbf6** at 2026-09-23T22:21:37Z

Phase 5 promote left this parked. The description says to park until the Phase 3 need is clear, and this promote did not establish that need. It stays draft.

**agent:cursor/6895** at 2026-09-24T03:17:22Z

Drew chose to groom TKT-01M3558E21PZDJEYAAR4WH9B85 (Soft-link git-ticket claims to lake session_uid) into a spike and promote it to ready. Phases 0–5 are closed and the ready backlog was empty. The Phase 3 and Phase 5 promotes left this draft parked on purpose: the old description said to wait until the Phase 3 need was clear, and those promotes did not establish that need. This request is that promotion.

The type is spike. The question label is gone because the spike type already says this is a question to answer. Labels that remain are area/ops and phase/3-export. The milestone stays phase-3.

The parent stays TKT-01M3558DZ6CBW2HB49WFFY20CP (Phase 3 export-based harnesses & conflicts). That epic is done and is not reopened. git ticket check --strict and git ticket doctor --strict did not flag a ready child of a done epic, so the parent link stays as lineage.

The spike decides whether a claim (an agent:terva/session-… actor, and schema-3 claim.session) should resolve to the lake session_uid. It does not implement the link. The earlier park notes still stand as the record of why it stayed draft through those promotes.

**agent:cursor/d336** at 2026-09-24T03:33:24Z

### Evidence for do-not-wire

This note is the investigation record. The decision itself is the architecture paragraph that begins "A git-ticket claim does not resolve to that uid."

Lampi already keys a terva transcript without reading a ticket. internal/adapter/terva nativeID uses the meta line id when that line has one, and the filename stem otherwise. internal/catalog lookupSession and Catalog.Current select session_uid by (harness, native_session_id). Alias selects by (harness, native_session_id, machine_id). session_uid is minted with internal/id.New at ingest. internal/normalize sessionID is the harness name, a colon, and the native id. A search of this module's Go finds no reader of a ticket, a claim, or an actor. The only git-ticket mention under internal/ is a comment in internal/cli/cli.go about how the command package is laid out.

git-ticket v0.23.0 documents the other id space. docs/plan.md shows a claim actor agent:terva/session-123 and a session value 01M1WQ1PWA0TPQATQ66978AVC2. testdata/parse/roundtrip/claim-session.md uses actor agent:terva/session-4417 and session 01M1VXVWZQ8K3PYRFN20HCTE5D, and says the value is free text that nothing parses. Section 6.4 says only the harness knows its session id.

terva's release branch, read for this spike and not vendored here, fills those fields differently from the git-ticket examples. packages/core/session.go NewSession names the file with a UTC timestamp and eight hex digits from one UUID, then newSessionAt mints a second UUID as Session.ID and as meta.id. packages/agent/build/lorewiring.go RebindTasks binds the task board to sess.ID. packages/agent/tools/ticket_write.go passes that board id as ClaimTicket.Session. packages/agent/tools/ticket_tasks.go returns an empty session id when there is no board, and says a made-up id would point at a transcript that does not exist. packages/agent/build/ticket_actor_test.go expects actors such as agent:terva/mieli and agent:terva/code-reviewer, not agent:terva/session-….

So the actor suffix and claim.session are not session_uid. claim.session equals native_session_id only when terva wrote the meta UUID and the lake ingested that meta line. The filename stem, the persona suffix, the session- label, an empty field, and the ULID in git-ticket's examples do not. Phase 3 closed without a consumer: export, sidecars, and the conflict list address session_uid or the native id. Nothing since then added a ticket reader.

The parent epic TKT-01M3558DZ6CBW2HB49WFFY20CP (Phase 3 export-based harnesses & conflicts) stays done.

## Summary

Decision: do not wire a soft link from a git-ticket claim to the lake session_uid.

The record is the paragraph in docs/architecture.md that begins "A git-ticket claim does not resolve to that uid." It sits with the Layer B identity rules. This summary points at that paragraph.

### How the ids relate

An agent:terva/session-… actor is the claim's holder, the shape git-ticket's examples use. Terva writes agent:terva/ plus a persona. The suffix is not (harness, native_session_id) and it is not session_uid. Schema 3 claim.session is free text that git-ticket does not parse. When terva fills it, the value is the transcript meta UUID, which the terva adapter already stores as native_session_id when that meta line is present. session_uid is a ULID minted at ingest for the pair (harness, native_session_id). Normalized events use terva: plus the native id. A claim has no machine_id, so it is not an alias key.

They do not match when the actor suffix is used as the lookup, when claim.session is empty because the session has no task board, when the value is the filename stem (YYYYMMDD-HHMMSS- plus eight hex digits) while the catalog holds the meta UUID, when the value was typed by hand, and when the value is a ULID of the kind git-ticket's examples use. That ULID shares an alphabet with session_uid and is not a uid this lake assigned.

### Why a soft link is the wrong join

The lake already learns the native id from the transcript bytes. The claim is a second copy of a string the producer does not promise: empty is normal, the documented examples are ULIDs, and the actor suffix is a different namespace. Treating claim.session as session_uid would join two independently minted ids. Treating the actor suffix as native_session_id would miss the catalog row. No package, command, or route in this tree reads a ticket. export, conflicts, and normalize already select a session by session_uid or by (harness, native_session_id). Wiring a resolver would make the lake parse a ticket store for a question those paths do not ask.

The wire clause does not apply. There is no lookup key, no harness binding, and no resolver. An unresolved claim creates nothing because no claim is consulted. This spike ships no catalog schema change, no claim-format change, and no production resolver, and it files no follow-up implementation ticket.

The parent epic TKT-01M3558DZ6CBW2HB49WFFY20CP (Phase 3 export-based harnesses & conflicts) stays done.
