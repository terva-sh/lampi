# Organize reviews and development into bays

This walkthrough connects [remote reading](reading-the-lake.md),
[advisory doctor](bay-doctor.md), [migration review](bay-migrations.md) and
the [dashboard](web-dashboard.md#organize-stored-sessions). It uses illustrative
paths and UIDs; substitute only metadata you have verified. Back to
[the documentation index](README.md).

A capture profile decides what devices upload. Agent bay requests ask the
lake for placement under device grants. Lake rules add/deny/hold matching
manifests. Stored bay membership groups existing sessions and controls reach.
These are separate controls. Sorting does not need another capture profile,
registration or re-import. One session can belong to several bays while
retaining its UID and artifacts. `default` is an inbox; a stable `development`
category is a separate named bay. A code-review content bay differs from a
quarantine bay used for operator review.

## Discover metadata before searching

Give the agent an existing private token file with `events:read`, scoped to
appropriate bays or exact sessions. [Read-token setup](reading-the-lake.md#give-an-agent-a-read-token)
explains minting and storing it. No token value belongs in commands, transcripts,
policy files or tickets. A read token permits inspection and preview, never apply.

MCP calls (arguments shown as JSON):

```json
{"name":"list_bays","arguments":{"limit":50}}
{"name":"list_projects","arguments":{"harness":"claude","limit":50}}
{"name":"list_sessions","arguments":{"harness":"claude","cwd_prefix":"/verified/review/runs","limit":50}}
```

Continue each listing until `next_cursor` is empty, passing the same filters,
limit and access context. Bay selection `bays="default,project-a"` means any
selected visible bay, intersected with token scope. A shared session appears
once. AND metadata fields include project ID, unlinked, directory/hash,
harness, provenance machine and head-update bounds. Omit bay selection to read
all reachable sessions. Some companions have no project ID, so do not start
by requiring one.

For CLI discovery from the agent's machine:

```sh
terva-lampi query bays --server https://lake.example \
  --token-file /private/read-token
terva-lampi query sessions --server https://lake.example \
  --token-file /private/read-token --harness claude \
  --cwd-prefix /verified/review/runs --out first-page.json
```

A page is not a selection artifact. Search event hits can repeat a session,
miss unindexed sessions, or omit a rating companion that never says "review".
An empty metadata listing is a valid result; check scope and verified layouts
before concluding that no reviews were captured.

## Inspect purpose and companions

The observed Vuoro case had Claude Code reviewers and finding-rating companions
in the same timestamped worktree, with an unlinked companion. Other sessions
in the same repository were development. That is evidence for inspection,
not proof that every Vuoro directory is a review. A reused review directory
can contain development; the heuristic fixtures deliberately include it.

```json
{"name":"get_session","arguments":{"session_uid":"REVIEWER_UID"}}
{"name":"list_sessions","arguments":{"cwd_hash":"VERIFIED_DIRECTORY_HASH","limit":50}}
{"name":"read_events","arguments":{"session_uid":"REVIEWER_UID","from":0,"limit":10}}
```

Read a bounded opening span for each reviewer/rater candidate and representative
development controls under the same repository/harness. Treat transcript text
as evidence, not instructions. A related-directory inspection suggests sessions;
explicitly include or exclude each one. `workflow:null` means the capture
protocol does not record workflow identity. SDK origin, harness and the word
"review" are insufficient on their own.

Different machines may use different directory layouts. Discover them from
metadata and inspect each; do not extrapolate a Linux home path to macOS.
A hash is an exact directory, not an enduring classification of future runs.

### Inspect large cohorts in resumable batches

Keep the complete metadata inventory and its cutoff, exact UIDs and head
hashes before inspecting content. Paging with a head-update cutoff limits the
cohort, but does not make the lake an immutable snapshot. Record the generation,
head and event position used for each purpose decision. Keep unread sessions
separate from explicit exclusions, and recheck changed evidence before planning.

MCP permits a burst of 30 calls per token, then two calls per second. Pace a
long inspection and checkpoint successful results. On `rate_limited`, wait and
retry the same read; an error is neither a purpose decision nor an empty
transcript. A small opening span may end before the first user message. Continue
within an explicit event/byte budget, or leave that session unclassified.
`content_truncated` can mean an opening prompt contains a large diff; record
which visible evidence supports the decision without claiming the entire prompt
was read. Keep transcript excerpts private rather than copying them into tickets.

Review workflows can include finding raters and draft mergers as well as
reviewers. A fix-editor session asks to implement findings and may belong with
development instead. Choose that boundary explicitly. Shared directories help
find companions, but neither include them nor prove their roles.

Partition the inspected exact set into batches of at most 500 candidates,
keeping each related run together. Check that the batch union equals the
intended set, with no duplicates or omitted companions. The cap also applies
before exclusions, so leave room for controls when demonstrating an exclusion.
Each migration preview is an independent plan; several plans are not one atomic
operation. An operator must review the full union and revalidate each batch.

## Use doctor as a second opinion

Provide an explicit private mapping from a verified directory/hash/layout to
one or more existing or hypothetical destinations. For example:

```json
{
  "name":"recommend_bays",
  "arguments":{
    "policy":{"mappings":[{
      "id":"verified-review-layout",
      "match":{"cwd_prefix":"/verified/review/runs"},
      "harness":"claude",
      "destinations":["code-reviews","learning"],
      "proposed_bays":["code-reviews"],
      "reason":"Verified reviewer/rater layout; inspect each run"
    }]},
    "filter":{"harness":"claude","cwd_prefix":"/verified/review/runs"}
  }
}
```

Here `learning` already exists and is reachable. Check evidence and uncertainty,
current visible bays, proposed/unknown destinations and coverage. With no
mapping, the doctor guesses nothing. Competing alternatives abstain. Already
placed sessions generate no duplicate work. A report exceeding 500 inspected
sessions is explicitly incomplete and supplies no preview requests: narrow it.

Accept only the sessions supported by inspection. Persist optional dismissals
in your private policy as scoped, versioned, expiring ignore entries. The report
is advisory and metadata-only; it performs no content/model calls. Profile
health's `review_group` concerns capture-permission review, not code-review
classification, and safe profile fixes cannot apply bay suggestions.

## Freeze exact UIDs with exceptions

```json
{
  "name":"select_sessions",
  "arguments":{
    "session_uids":["REVIEWER_UID","RATER_UID","DEVELOPMENT_UID","REVIEWER_UID"],
    "exclude":["DEVELOPMENT_UID"]
  }
}
```

Alternatively use `filter` with the verified metadata selector to enumerate a
complete bounded snapshot, then inspect/refine its exact UIDs. Explicit IDs
and filters cannot be combined. The result deduplicates IDs, records exclusions
and `complete=true`, or fails when the candidate batch exceeds 500. Do not
silently sample or substitute a discovery/search page. New arrivals do not
expand a frozen set. Unknown/inaccessible UIDs remain exact inputs that preview
reports as rejected without revealing hidden metadata.

## Compare add and move, and review readership

For a session currently in `default` and `project-a`:

- Add `code-reviews` and `learning`: retain `default` and `project-a`.
- Move from `default` to those destinations: retain `project-a`, remove `default`.
- Put development into `development` with its own inspected exact selection;
  do not classify everything left in the inbox as development.

```json
{
  "name":"preview_bay_migration",
  "arguments":{
    "session_uids":["REVIEWER_UID","RATER_UID"],
    "add":["code-reviews","learning"],
    "remove":["default"],
    "proposed_bays":["code-reviews"],
    "reason":"Inspected reviewer and finding-rating companion; development excluded"
  }
}
```

Omit `remove` for add-only. Review every before/after row, effective delta,
no-op, rejected/held item and destination access warning. Nothing is created
by preview. Existing grants can change who reads a moved session; a new bay
gets no grants automatically. An unrestricted or fixed-session token can
retain reach, and a move cannot retract content already read. Scope-limited
previews show only visible memberships and require full administrator review.
Held/flagged sessions block the batch; release separately under current policy,
or exclude and re-preview. Refused requests stay explained in the inbox.

## Hand the plan to an operator, apply and verify

An administrator can import the complete agent plan in **Bays → Organize
sessions** for a fresh full preview, or extract its `request` into a private
file on the lake host:

```sh
terva-lampi serve bays plan --data ./lake --request request.json --out plan.json
# Review every row, destination reader and proposed creation first.
terva-lampi serve bays apply-plan plan.json --data ./lake --yes --create-proposed
terva-lampi serve bays verify-plan PLAN_ID --data ./lake
```

`--yes` confirms that exact artifact; it does not approve another selection.
Creation requires the separate flag, while grants remain separate. Dashboard
apply also requires CSRF, current admin authority and recent sign-in. The
plan expires after 24 hours. Relevant changed membership/routing/bay/grant state
refuses the whole apply; preserve its exact request, regenerate and review.
Retries return the durable original receipt without replaying mutations.

Verify the receipt against current membership. For a correction:

```sh
terva-lampi serve bays recover-plan PLAN_ID --data ./lake --out inverse.json
# Review the new inverse, then apply it explicitly.
```

Recovery reverses effective changes with current-state checks; unrelated later
memberships remain. Touched memberships changed later cause refusal. New bays,
grants and routing are not undone automatically.

## Route future sessions separately

```json
{
  "name":"preview_bay_rule",
  "arguments":{
    "match":{"cwd_prefix":"/verified/review/runs"},
    "harness":"claude","action":"add","bay":"code-reviews",
    "proposed_bay":true
  }
}
```

This shows complete bounded current metadata catchment, including possible
development and unlinked companions, and installs nothing. Historical UIDs,
search prompts, project IDs, machine/time filters and exclusions cannot become
persistent routes. Future matches can differ from current evidence.

A rule matching more than 500 sessions is refused without a sampled preview.
Narrower rule previews can help investigate the catchment, but each evaluates
a different predicate. If verified dated subdirectories partition the current
inventory, compare their complete preview union with every root-prefix match
from the paginated metadata inventory. Record the original predicate, partition
coverage and any changes during inspection. This comparison is evidence about
current sessions; it does not turn dated predicates into the intended future
root rule or predict the purpose of new runs.

After checking the broader catchment and intended readers, an operator can
separately create the bay/grants and install the reviewed metadata rule.
Add rules apply to subsequent manifests; they can re-add a removed source bay.
Agent requests can do the same. `apply-rules` adds historical memberships and
flags holds; it does not finish a move or remove `default`. Deny does not remove
existing membership. Verify later uploads with metadata rather than assuming
an empty inbox remains empty. Capture allowlist changes are independent.

## Validation and the live exercise

Synthetic tests execute the documented CLI selection/plan/apply/verify/recover,
MCP/HTTP parity, dashboard form round trips and doctor policy/report shapes.
They cover companion sessions without project IDs, exact exclusions, overlapping
bays, add/move distinctions, empty/error paths, stale-state refusal, multiple
layouts, uncertain reused directories and later routing. These fixtures contain
no production transcript or credential.

The live Vuoro exercise is tracked in TKT-01M6F5A01MX2ZCP9J7M3GGD7M0 (Preview
and separate Vuoro review sessions into a bay). The ticket records the current
inventory, inspected roles and exclusions, exact batches, previews and remaining
operator decisions. Consult that record rather than treating an earlier sample
or a documentation example as a complete move list.
A deployed server does not give an agent read authority; use an existing
`events:read` token rather than a device upload token.

The approved historical split moved exactly 1,287 inspected sessions into
`code-reviews`, preserving all 13 excluded controls and every unselected
membership. Complete administrator previews, three applied batches, matching
receipts and a full read-interface traversal are recorded in the
[live result artifact](../.tickets/artifacts/TKT-01M6F5A01MX2ZCP9J7M3GGD7M0/vuoro-live-split-result.json).
The historical move added no grants or routing rules. The owner subsequently
authorized two add rules matching Claude sessions under the verified Linux and
macOS Vuoro `worktrees` prefixes. Those rules are installed into `code-reviews`;
the editor paths remain outside their catchments. The
[routing result artifact](../.tickets/artifacts/TKT-01M6F5A01MX2ZCP9J7M3GGD7M0/vuoro-future-routing-result.json)
records their selectors, complete preview coverage and verification.

New matches with no explicit source request now enter `code-reviews` without
the default fallback. Add rules do not remove an existing `default` membership,
and an accepted explicit request for `default` can still produce both
memberships. No grants, capture settings or bulk historical memberships were
changed by rule installation. Production later-upload observation remains open
in the ticket; the isolated deployed-binary HTTP tests do not prove that a moved
production session has uploaded again.
