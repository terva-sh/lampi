# Advisory bay destination doctor

Bay doctor identifies metadata that matches an explicit user mapping. It does
not identify a review merely because it ran Claude Code, used an SDK, lived in
a repository, or mentioned reviews. Capture-profile health and its safe fixes
remain separate. No doctor suggestion writes memberships, grants, routes,
profiles or bays. Back to [the documentation index](README.md).

## Evidence and evaluation

Recorded workflow identity would be stronger than directory layout. Capture
protocol 1 currently has harness, project cwd/hash/remote, requested bays and
machine provenance, but no trusted workflow/role/run identifier. Discovery
therefore returns `workflow: null`. A mapping is an operator's policy about
observed metadata, not new provenance. Two sessions sharing a directory may
be related; that does not prove purpose or include the companion automatically.

`mapped-metadata-v1` requires a directory prefix, exact hash or glob plus an
explicit destination mapping and reason. Optional harness/remote fields
corroborate and narrow it. All match fields combine with AND using the existing
routing matcher. Other machine layouts need separate verified mappings.
Missing or clipped evidence, unknown destinations and conflicting alternatives
remain visible. There is no numeric confidence score and no automatic action.

The executable sanitized matrix in `internal/baydoctor/doctor_test.go` labels
11 cases: reviewer and unlinked finding-rater; development in the same repo;
development discussing review; another SDK job; reused review directory;
separately mapped macOS review; unknown machine layout; missing directory;
wrong harness; and a lookalike directory prefix. Metadata matching yields
3 true positives, 1 false positive, 2 false negatives and 5 true negatives.
The false positive is development in a reused review directory. The false
negatives lack a mapped layout or directory. These results reject autonomous
purpose classification: every match remains an uncertain advisory suggestion.
They are fixture results, not an estimate of production accuracy.

Additional fixtures verify multiple compatible destinations, correctly placed
sessions, unknown/inaccessible and hypothetical bays, alternative conflicts,
metadata clipping, incomplete inventory, scoped expiring dismissal and stable
finding IDs. Content-assisted classification is deferred: it would need
`events:read`, explicit consent and a bounded event/token budget. This version
reads no content and incurs no model calls or search-index dependency. A person
or authorized agent can inspect opening prompts separately, including development
controls, then accept only the exact sessions supported by that inspection.

## Supply a private policy

```json
{
  "mappings": [
    {
      "id": "review-layout-linux",
      "match": {"cwd_prefix": "/verified/review/runs"},
      "harness": "claude",
      "destinations": ["code-reviews", "learning"],
      "proposed_bays": ["code-reviews"],
      "reason": "Operator-verified review layout; inspect each run"
    }
  ]
}
```

Here `learning` exists and is readable; `code-reviews` is hypothetical.
Use only verified paths for your own devices. No built-in name/path is assumed.
Proposed names remain hypothetical even when a scoped caller cannot determine
availability. A full administrator preview checks collisions before apply.
An unknown and an inaccessible existing destination produce the same status
and no preview request. Already-present destinations produce no duplicate work.

Multiple destinations are compatible additions. To describe competing choices,
set the same `alternative_group` on separate mappings. If two such mappings
match a session, the doctor abstains with `conflicting_alternatives` rather
than treating both choices as required. Resolve the policy or make an explicit
historical selection before proceeding.

The policy is at most 64 KiB, with at most 20 mappings and 500 ignore entries.
Each mapping has up to 20 destinations. Keep policy/report files private; reasons
should contain observations and decisions, not transcript dumps or credentials.

## Run the same report from each surface

On the lake host:

```sh
terva-lampi serve bays doctor --data ./lake --policy policy.json
terva-lampi serve bays doctor --data ./lake --policy policy.json --json
```

Remotely, using an existing private `events:read` token file:

```sh
terva-lampi query recommend-bays --server https://lake.example \
  --token-file /private/read-token --policy policy.json --out report.json
```

Both accept exact repeatable `--session UID`, or AND metadata filters
`--harness`, `--project`, `--unlinked`, `--cwd-prefix`, `--cwd-hash`, `--machine`,
`--updated-since`, `--updated-until`. Exact UIDs cannot combine with filters.
`--bays NAME,ID` means any selected visible bay, intersected with access.
`--out` writes atomic private JSON; otherwise use `--json` for JSON or omit
it for human output. No `--write` or safe-fix mode exists.

MCP tool `recommend_bays` and POST `/api/read/v1/recommend-bays` take:

```json
{
  "policy": {"mappings": []},
  "filter": {"harness": "claude"},
  "bays": "default"
}
```

Replace the empty mapping list with your reviewed policy. An empty policy
returns no suggestions and explains that no destinations were guessed.
Use `session_uids` instead of `filter` for an exact inspected cohort.
The report reads only reachable sessions and visible memberships/destinations;
it reveals no hidden co-memberships or profile/device-wide inventory.

Administrators open **Bays → Organize sessions → Bay doctor**. The profile
**Health** panel links there without treating stored sessions as that profile's
inventory. Paste policy JSON and inspect the current selection (when nonempty)
or current metadata filters. Each eligible finding offers a complete exact
add preview. Choose explicit Move sources in the organization form when inbox
removal is intended. Suggestions never imply a future route.

## Interpret and refine the report

Reports carry `heuristic_version`, `scope`, `as_of`, `complete`, `inspected`,
`unmatched`, `already_placed`, `suppressed`, `findings`, `abstentions`, `warnings`.
Findings have stable `id`, `code=bay_destination_mapping`, version, session UID,
current visible memberships, destinations, proposed bays, evidence, uncertainty,
status and an optional exact `preview_request`. The ID includes evidence,
version and destinations. Reordering equivalent destinations preserves it.

A report inspects at most 500 metadata rows across bounded pages. If there are
more, `complete=false`, findings are marked `incomplete_inventory` and have no
preview handoffs. Narrow the request or use exact smaller cohorts. Metadata
can change during paging; treat the report as advisory, freeze and inspect
exact UIDs, and revalidate via the migration service before apply. Search-index
coverage cannot omit sessions from this metadata report. Clipped metadata
produces `incomplete_metadata` abstention.

To dismiss a finding, retain feedback in your private policy, for example:

```json
{
  "mappings": [],
  "ignore": [{
    "finding_id": "COPY_FINDING_ID",
    "scope": "COPY_REPORT_SCOPE",
    "version": "mapped-metadata-v1",
    "until": "2026-12-01T00:00:00Z"
  }]
}
```

Keep your mappings alongside the ignore entry. Dismissal applies only to that
exact finding, scope and heuristic version until the explicit timestamp. It
is request-local: neither MCP nor the dashboard writes shared feedback. Expired
feedback shows the suggestion again. Changed evidence/destinations produces a
new finding. This avoids silently suppressing a different policy or user.

Review a finding's `preview_request` with `serve bays plan --request FILE`,
or pass its exact UID/destinations to `preview_bay_migration`. Add explicit
source removals only when a move is intended. Scoped plans require a fresh
full administrator preview. Read tokens and plan artifacts grant no mutation
authority. Historical add/move, future routing and capture permission remain
three separate decisions. See [migration review and recovery](bay-migrations.md).

When several compatible mappings overlap, their combined destinations must
still fit the migration service's limit of 20. A larger union produces
`destination_limit` without a preview request; refine the policy first.
