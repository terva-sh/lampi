---
schema: 3
id: TKT-01M3558E21PZDJEYAAR4WH9B85
title: Soft-link git-ticket claims to lake session_uid
type: spike
status: ready
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
updated_at: 2026-09-24T03:17:22Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/6895
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

- [ ] Record how an agent:terva/session-… actor and schema-3 claim.session relate to (harness, native_session_id) and to session_uid, including when they do not match.
- [ ] Record exactly one decision: wire a soft link, do not wire one, or defer.
- [ ] When the decision is wire, name the lookup key, the harness, whether the key matches native_session_id or session_uid, and that an unresolved claim does not create a lake session.
- [ ] When the decision is do not wire or defer, state why. A defer also states the criteria that would open a later implementation ticket.
- [ ] Put the decision in the ticket summary, or in docs/architecture.md with the summary pointing at that paragraph.

## Definition of done

- [ ] The decision is recorded in the summary, or in an architecture note the summary cites.
- [ ] This spike does not ship a catalog schema change, a claim-format change, or a production resolver. If the decision is to wire, the implementation is a separate draft ticket.
- [ ] git ticket check passes after that record lands.

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
