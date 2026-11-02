# Find sessions and organize their bays

This is the interaction contract for the session organization work. The
examples below describe the intended workflow; the command reference
identifies the commands available in each release. Back to the
[documentation index](README.md).

A capture profile decides what agents upload. Bays organize sessions
already in the lake and control who can read them. A session can belong
to several bays without copying its artifacts or changing its identity.
The default bay is an inbox. Give development work its own named bay
when it needs a stable category distinct from unsorted sessions.

## Start with evidence

The first observed case is Vuoro reviews recorded by Claude Code. A
reviewer and its finding-rating companion shared a timestamped Vuoro
worktree. The companion had no project ID. Other sessions in that same
repository were development work. These are observations of sampled
sessions, not proof that every session from Vuoro is a review.

| User job | Useful evidence | Check before selecting |
|---|---|---|
| Sort a review run | Working directory, harness, native session IDs | Include the reviewer and finding-rating companions |
| Sort reviews from several machines | Verified directory layouts and origin evidence per machine | Do not assume the same home directory or layout |
| Split work in one repository | Directory, explicit workflow provenance, inspected opening prompt | A repository or harness alone can include both kinds of work |
| Classify a session with no project | Session ID, directory, related-run evidence | A missing project ID is not a reason to omit it |
| Select reviewed search results | Deduplicated session IDs from the hits | A page of event hits is not the complete session inventory |
| Add another useful category | Current visible membership and intended readership | Retain existing memberships unless removal was requested |
| Correct a mistaken move | Previous plan and current membership | Preserve unrelated changes made after the original operation |

Explicit recorded workflow provenance is the strongest evidence when
it exists. A known directory layout plus corroborating session evidence
can support a suggestion. An SDK origin, the word "review", or a bay's
name alone cannot establish session purpose. Missing and contradictory
evidence must remain visible. A suggestion can abstain.

## Selection rules

Field filters combine with **AND**. Repeated selected bays mean **any
of these bays**, intersected with the caller's existing access. A
session in several selected bays appears once. The same rule applies
before counts, statistics and diagnosis are calculated.

Exact session IDs form a set. Exclusions remove IDs from that set.
Repeated IDs do not duplicate work. An inaccessible ID is treated like
an unknown ID. Discovery uses bounded pages with an explicit
continuation; a partial page or a truncated diagnosis is not a complete
inventory. A continuation must retain its filters and access context.

"Select all matching" means consume the complete filtered listing and
freeze the resulting IDs for review. Show the total, the exclusions,
and whether enumeration completed. A practical size limit requires
another explicit batch, never a silent sample. New matches arriving
after the preview are not included in apply.

Show related sessions as suggestions with their reasons. A user chooses
whether to include them; related-run evidence must not silently expand
the reviewed set. Directory/remote/harness predicates may support
future routing. An arbitrary UID set, a search query or an inspected
prompt only identifies a historical selection unless a separate
recorded routing field supports it.

## Choose an operation

**Add to bays** adds all chosen destinations and retains all current
memberships. **Move from bays** removes only the explicitly chosen
source memberships and adds all chosen destinations. Unrelated bays
remain. The destinations are compatible memberships, not a choice
between alternative classifications. Conflicting recommendations must
be resolved before they become an operation.

For example, with a session initially in `default` and `project-a`:

- Adding `code-reviews` and `learning` retains `default` and `project-a`.
- Moving from `default` to those two bays retains `project-a` and removes
  `default`.
- Moving back later uses the current state and a new preview, so a bay
  someone added in the meantime remains.

A held session needs an explicit release under the existing hold
policy. Sorting cannot bypass it. A session must retain at least one
membership; explain the existing default fallback whenever it applies.
Duplicate destinations are no-ops. Using a bay as both a removed source
and a destination is an input error, rather than a hidden precedence
rule. A proposed new bay can appear in a preview without being created.
Creation and grants are explicit operator actions.

## Review, apply and verify

The CLI and dashboard follow the same steps:

1. Discover bays, projects and sessions; inspect explicit metadata.
2. Include/exclude exact sessions, with a complete selection count.
3. Choose add or move, sources and one or more destinations.
4. Preview each membership before and after, matching reasons,
   no-ops, blocked items and the relevant access implications.
5. Apply the reviewed plan with operator authority.
6. Verify actual membership and any refused or failed items.

An agent uses read-only discovery and preview tools for steps 1–4 and
hands the same plan to the operator. A plan is data, not write authority.
Do not trust one merely because an agent produced it. Applying it must
validate its contents, the current operator, destinations, holds and
the membership state that was reviewed.

The plan records exact IDs and the state its changes rely on. Changes
to relevant memberships, destinations, aliases or routing invalidate
the preview and require another review. Appends that leave the relevant
state intact need not invalidate an otherwise identical operation.
Retries must report already-applied no-ops without widening the set.
Bound transactions and make partial progress explicit when a workflow
requires multiple batches. A correction or inverse plan uses a fresh
state check rather than restoring an old snapshot over later edits.

Bay membership is an access boundary. Explain which authorized groups
or tokens gain or lose reach where the caller may inspect that policy.
An unrestricted token or a token scoped to fixed session IDs can still
read a moved session. A move cannot retract content someone already
read. A scoped agent must not learn hidden co-memberships or unrelated
grant/device information merely to explain its own selection.

## Dashboard behavior

Start the flow from a bay, session, search results, the inbox or a doctor
finding. Use one selection model across those entry points. Show a
persistent selection count and exceptions across pages, with explicit
"this page" versus "all matching" controls. Retain selection after a
recoverable validation error. Show empty, loading, incomplete and stale
states in text.

Use labeled checkboxes for sessions and destinations, a labeled add/move
choice, and explicit source controls for a move. Everything must work
by keyboard. Announce selection changes, preview errors and completion
to assistive technology. Preview includes a readable membership change
for every session, rather than relying only on color. Existing recent
sign-in, CSRF and admin requirements remain in force.

The inbox also explains refused bay requests and holds. A bay for code
reviews is a content category; a bay holding quarantined sessions for
operator review has a separate purpose. Preserve the explicit release
step in bulk triage.

## Route future work separately

A one-time move sorts stored sessions. A lake add rule or an agent bay
request controls subsequent manifests. Creating or applying an add
rule does not remove existing default membership. Conversely, moving a
session does not prevent an existing request or rule from adding the
source bay again on a later upload.

Offer a separate future-routing preview, including the broader
metadata match it would admit and all observed device layouts. Do not
turn a text-based selection into a broad repository rule. Explain the
effect on new sessions and updates to existing ones. Capture allowlist
changes remain a separate decision.

## Decisions and tradeoffs

Filter-only moves cannot express an exact reviewed subset with
exceptions. Automatic membership replacement discards useful secondary
bays. Re-running a live query during apply changes the reviewed set.
Keyword-only classification confuses development discussing reviews
with a review run. The workflow therefore uses exact reviewed sets,
explicit source removal and explainable advisory classification.

The implementation tickets own concrete schemas, bounded batch sizes
and tests. Heuristic evaluation must determine useful evidence and
abstention cases using sanitized positive and negative fixtures; no
production transcript is required as a fixture. Missing provenance is
reported as missing rather than inferred from a display label.
