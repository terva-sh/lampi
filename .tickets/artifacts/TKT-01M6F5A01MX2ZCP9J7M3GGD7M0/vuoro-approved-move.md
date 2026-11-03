# Approved Vuoro move operator procedure

The owner approved exactly the frozen 1,287 sessions in
`vuoro-proposed-split.json`: remove `default`, add a new `code-reviews`
bay, preserve unrelated memberships, and create no reader grants.
Future routing, profiles, agents and services are outside this operation.

Use the adjacent `vuoro_approved_move.py` on the documented lake host.
It requires sudo and uses `/usr/local/bin/terva-lampi` as `terva-lampi`.
It never accepts a password itself. Keep credentials out of commands,
tickets and artifacts; use private credential delivery for sudo.

1. Run `sudo /usr/bin/python3 -I RUNNER preview --proposal PROPOSAL
   --sha256 1c93128d1c84225ecf59bc740ee714e72f9c4a2543b62fc2be0d5ddb6eae6b4f`,
   replacing RUNNER and PROPOSAL with absolute paths to the adjacent files.
2. Read the returned work directory's full `preview-1-plan.json` through
   `preview-3-plan.json`, `before-grants.stdout`, `before-rules.stdout`,
   `before-list.stdout`, and `before.json`. Review every effective
   before/after membership, access warning, installed routing rule and
   selected manifest bay request. Confirm the exact 500/500/287 cohorts,
   full administrator visibility, no exclusions, and compatibility with
   the approved access change. Do not apply an incomplete or incompatible
   preview. A verified catalog backup and completion manifest must exist.
3. Run `sudo /usr/bin/python3 -I RUNNER apply --work WORK`. Each batch
   gets a fresh full plan; later batches use the destination created by
   the first. The runner pins its identity and checks grants/rules before
   every apply. CLI transactions also reject stale plans. The three
   batches are separate transactions, so interruption can leave a partial
   move. The runner does not install rules or grant access.
4. Run `sudo /usr/bin/python3 -I RUNNER verify --work WORK`. Review the
   receipts, actual membership, exclusions, grant/rule inventory and
   reported `complete`, `moved`, `remaining`, and later manifest count.
   Zero later manifests is not evidence about real subsequent uploads.

The runner pins the original proposal digest in every phase, including
the exact batch partition. The completion manifest binds the preview
plans, baseline inventories and checked backup. Apply refuses work whose
preview artifacts changed or whose apply was already attempted.

After any interrupted apply, use **verify on the same work directory**
before recovery. It looks up catalog receipts using the plans saved
before each transaction, even if the process lost CLI stdout. Each
successful CLI receipt is also saved immediately before validation.
Verification works for zero, one, two or all three committed batches and
reports remaining UIDs without replaying mutations. Preserve the work
directory. Investigate any mismatch or policy change before proceeding.
Completing remaining approved UIDs needs fresh administrator plans that
account for the existing destination; this runner deliberately provides
partial verification rather than automatic resume. Do not replay the
entire original selection, restore the catalog, or apply inverse plans
as an automatic response to a partial move.

`python3 -I test_vuoro_approved_move.py` rehearses the operation against
isolated synthetic schema-23 lakes using the deployed CLI. It tests the
full move, pinned input, incomplete preview, changed backup, lost output
after a committed batch, and a concurrent grant before batch two. The
tests substitute privilege dropping and the synthetic proposal digest;
they do not establish production service-account access or a live move.
