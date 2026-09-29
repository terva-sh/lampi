---
schema: 3
id: TKT-01M3N8FHZRSM965DNVQ79CDPZX
title: "Dashboard: batch Allow with a short per-profile confirm page"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
assignees: []
milestone: null
parent: TKT-01M3N8F354GDK9D0CTQ2BMZBMY
origin: null
dependencies:
  - TKT-01M3N8FHVNFEZ5N576RQ7CAG4E
  - TKT-01M3N8FHQD44ER3CZGZQCTW6G2
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T00:19:22Z
updated_at: 2026-09-29T01:14:42Z
created_by:
  id: agent:claude-code/10adf304
  name: ""
updated_by:
  id: agent:claude-code/10adf304
  name: ""
extensions: {}
---

## Description

Replace one-at-a-time Allow with a selection and one short confirmation.

- Checkboxes on `/review` rows and on refused rows of a device's page, with Select all on the page. **Allow selected…** posts the selection.
- The lake re-checks each selected project against the newest inventories, as single Allow does: still refused, allowable, and not already covered. It reports any that no longer qualify instead of failing the whole batch.
- It builds one rule per project with `allowRule`, and groups the rules by the profile of each device holding the project. A project on devices with different profiles adds its rule to each of them.
- **The confirm page**, not the profile editor: for each affected profile, the rules to add, the devices it reaches, the count of those whose `config.json` sets their own allow rules, and one prefilled, editable note. **Save** and **Back** go to the page the operator came from. An **Edit in full editor** link per profile opens today's editor, carrying the return from the back-navigation ticket.
- Save writes every profile, each against the revision its preview read, in one catalog transaction. If any profile moved, nothing is saved, and the page re-renders against what is stored now. Each saved profile gets its own revision and audit entry, as the editor's save does.
- Saving needs the same recent IdP sign-in as other operator actions that add access. The page offers Sign in again and comes back with the selection intact.
- Afterwards the operator returns to the review page with a notice. The allowed projects show as *allow pending* until their agents report.
- `POST /api/web/v1/review/allow` has a preview/save split mirroring the profile API. Documented in `docs/web-api.md` and `docs/web-dashboard.md`.

## Acceptance criteria

- [ ] Operators select several refused projects and allow them in one confirm
- [ ] The confirm page shows each profile's added rules and reached devices, with Back
- [ ] Save writes every profile against its read revision, all or nothing
- [ ] After saving the operator returns to where they started
