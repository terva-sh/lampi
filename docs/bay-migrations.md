# Review and apply a session migration

A profile controls capture. A bay groups stored sessions and controls access.
This workflow changes membership while preserving session IDs, artifacts and
unrelated memberships. See [identification and UX rules](bay-workflow.md) and
[remote discovery](reading-the-lake.md#discover-before-searching).

## Freeze the historical selection

An event search supplies evidence, not a complete session inventory. Inspect
session metadata and opening prompts, including unlinked reviewer/rating
companions. Then freeze exact UIDs. On the lake host:

```sh
terva-lampi serve bays select --data ./lake \
  --harness claude --cwd-prefix /home/me/.local/state/vuoro/worktrees \
  --out candidates.json
```

Metadata fields combine with AND. `--bays default,project-a` means any selected
bay, intersected with access. Selection uses one catalog query snapshot, records
the selector, deduplicates UIDs and applies explicit `--exclude UID` flags.
The complete JSON has `session_uids`, `excluded`, `complete`, `reason`, `scope`
and `selector`. All matches are included or the operation fails. A batch is
limited to 500 candidates; use narrower filters or explicit UID batches for
larger inventories. New matching sessions do not expand an existing selection.

For an inspected list, use repeated `--session UID`, or a file containing one
UID per line with `--sessions-file FILE`. A discovery page with a continuation
is refused as a selection file. A complete selection is still a candidate
set: inspect it before applying, and do not infer purpose from a directory or
harness alone. Files contain session metadata and should stay private.

## Preview additions or moves

```sh
terva-lampi serve bays plan --data ./lake --sessions-file candidates.json \
  --add code-reviews,learning --from default --propose code-reviews \
  --reason 'Reviewed Vuoro reviewer and rating companions' --out plan.json
```

Here `learning` already exists. `code-reviews` is proposed without creating it.
Omit `--from` to add destinations while retaining all existing memberships.
With `--from`, only named source memberships are removed. Shared sessions are
edited once; duplicate IDs/aliases become no-ops. An ID resolving to both an
addition and a removal is refused. Removing every named membership falls back
to `default`; removing the last default membership alone is rejected.

The version 1 plan is a complete bounded artifact, with:

| Field | Meaning |
|---|---|
| `plan_id` | SHA-256 checksum of the canonical plan; identifies its receipt after apply |
| `created_at`, `expires_at` | Preview lifecycle; expires after 24 hours |
| `request` | Exact UIDs, explicit exclusions, additions/removals and proposed bays |
| `items` | Every selected UID with visible `before`, `after`, effective `add`/`remove`, status, inclusion reason and administrator-only state fingerprint |
| `changed`, `noop`, `rejected` | Complete item counts, with no sampled preview |
| `destinations` | IDs/names, whether creation is proposed, and access implications |
| `scope`, `complete_memberships` | Access context and whether a complete administrator preview is available |
| `policy_state` | Administrator-only fingerprint of lake identity, bays, aliases, grants and rules |
| `warnings` | Access, future-manifest and recovery limits |

Statuses are `change`, `noop`, `unknown_or_inaccessible`, `held_or_flagged`,
`last_default_bay` or `needs_admin_review`. An advisory removal of every visible
membership reports `needs_admin_review`, with no inferred default fallback or
last-bay verdict. Hidden memberships and holds cannot affect advisory outcomes;
full session-state fingerprints are reserved for administrator plans. Keep the
exact request and regenerate an administrator preview to resolve
`needs_admin_review`. Unknown and inaccessible UIDs are indistinguishable. Any
rejected item blocks the entire apply: exclude or resolve it, then preview
again. Sorting never releases a hold. Destination errors conflate unknown and
inaccessible bays. A complete host preview refuses a proposed name already used by a bay or alias.
Advisory previews keep proposed names hypothetical and leave availability to the
administrator, so collisions cannot expose hidden bay names.

Plans carry no transcript text or credential and grant no authority. A checksum
is integrity/reproducibility data, not a signature or permission. A host operator
must review an agent-supplied artifact. All read-token previews, including
unrestricted tokens, show visible memberships and generic readership only. They
require a fresh full administrator preview before apply; the
portable `request` contains the exact intended changes for that handoff.

Access warnings matter: destination readers gain reach; removing a membership
can remove source-only readers' reach. Administrators, unrestricted tokens and
fixed-session tokens can retain access. Inspect `serve bays grants` to review
specific grants. A new bay receives no grants automatically. Removing a
membership cannot retract content already read.

Advanced callers can pass `plan --request request.json`. A request uses
`session_uids`, `exclude`, `add`, `remove`, `proposed_bays` and `reason`, or
`edits: [{session_uid,add,remove}]` for differing per-session edits. Per-session
edits cannot be combined with the uniform UID/add/remove form. A `recovery_of`
value records provenance; it does not grant authority or relax validation.

## Apply, verify and recover

```sh
terva-lampi serve bays apply-plan plan.json --data ./lake \
  --yes --create-proposed
terva-lampi serve bays verify-plan PLAN_ID --data ./lake
terva-lampi serve bays recover-plan PLAN_ID --data ./lake --out inverse.json
# Review inverse.json before applying it:
terva-lampi serve bays apply-plan inverse.json --data ./lake --yes
```

`--yes` confirms the reviewed artifact. `--create-proposed` separately permits
the proposed creations; grants remain separate. Apply requires current host or
signed-in administrator authority. Read tokens cannot apply. All edits, new
bays, individual membership audits and the durable receipt commit in one
transaction. There is no partial success inside a batch. Database/audit-queue
failures roll it back. If flushing `audit.jsonl` fails after commit, the CLI
reports that the migration committed and audit stays queued; retry safely.

Before writing, apply regenerates the plan from its exact request inside the
transaction. Memberships (including timestamps), holds, routing metadata and
requests must still match. Bay names/aliases, destinations, grants, lake identity
and routing rules must still match. Policy comparison is conservative: even an
unrelated bay/grant/rule edit can require a fresh preview. Appends that leave
routing metadata and memberships unchanged do not expand or invalidate the set.

A retry returns the durable original receipt with `already_applied=true`; it
never replays changes, even after expiry or subsequent work. This is historical
confirmation, so use `verify-plan` to inspect current membership. Receipts survive
server restarts and backups in the catalog and have no automatic expiry.

Recovery reverses only effective additions/removals. It checks touched membership
state, including remove/re-add timestamps, and refuses conflicting later edits.
Unrelated later memberships survive. The inverse is a fresh preview with normal
state/hold/authority checks at apply. Newly created bays remain, and grants and
routing are never restored or removed by recovery.

Every `--out FILE` is atomic and mode 0600; failure leaves the previous file
untouched. Stdout is one JSON object. Plan output and input artifacts are bounded to 8 MiB; use a smaller batch
if the complete membership preview exceeds that bound.
Do not replace the live agent or lake to try a development binary; use an
isolated synthetic lake as described in [development](development.md).

## Preview future routing independently

```sh
terva-lampi serve bays preview-rule --data ./lake --to code-reviews \
  --rule-action add --harness claude \
  --cwd-prefix /home/me/.local/state/vuoro/worktrees --out routing.json
```

Add `--proposed-target` if the named bay is hypothetical. This evaluates a
metadata predicate against all reachable stored sessions, independently of
historical selections. `complete=true` means the complete bounded catchment;
over 500 matches is refused. Narrow it before considering a rule.

`--cwd-glob`, `--cwd-hash`, `--git-remote`, `--git-remote-prefix` and `--harness`
are supported. Project IDs, text searches, exact UID sets and provenance-machine
selection are historical selectors, not persistent routing predicates. Compare
the catchment with both reviewed and development examples; a folder-wide rule
can also match future development work and unlinked companions.

Preview installs nothing. `serve bays rule` remains the separate explicit
installation operation. Once installed, an add rule also adds on a later
manifest of an existing session; deny rules do not remove existing membership;
hold rules quarantine new matches and flag stored ones. Manifest bay requests
can also re-add a removed membership. Historical sorting never edits profiles,
manifest requests or future rules automatically.

## Use the dashboard

Administrators can use **Bays → Organize sessions** for the same complete
selection, plan, apply and receipt workflow. Checkboxes retain exact UIDs
across pages; **Select all filtered** freezes the complete bounded metadata
set. Explicit exclusions and related-directory inspection handle unlinked
companions. See [dashboard organization](web-dashboard.md#organize-stored-sessions)
for keyboard controls, hold triage, private plan handoffs and future routing.

Discovery and selection also accept `--updated-since` and `--updated-until`.
Use UTC dates or RFC3339 times; the upper date includes its whole day, while
an upper timestamp is exclusive. These filter lake head-update times.

## Deployment and access review

This release upgrades the catalog from schema 22 to 23, appending
`bay_migrations` to retain durable plan receipts. Upgrade the lake under its normal backup/migration procedure;
a previous binary cannot open the newer schema. Rollback requires the catalog
backup, not simply replacing the executable. The normalizer and search-index
formats are unchanged by session organization.

Full administrator plans name bounded source and destination read-grant
principals alongside general token/admin reach. Removing a source can reduce
those readers' reach only when another membership or token scope does not
retain it. A clipped principal list explicitly directs the operator to
`serve bays grants` for the complete policy. All read-token previews, including
unrestricted tokens, disclose no grant principals or lake-wide policy fingerprint
and cannot serve as a complete administrator preview. Administrator authority
is established separately from the scope of session and bay reads.
