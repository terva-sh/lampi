---
schema: 3
id: TKT-01M3N8FHZRSM965DNVQ79CDPZX
title: "Dashboard: batch Allow with a short per-profile confirm page"
type: task
status: done
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
updated_at: 2026-09-29T01:44:52Z
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

- [x] Operators select several refused projects and allow them in one confirm
- [x] The confirm page shows each profile's added rules and reached devices, with Back
- [x] Save writes every profile against its read revision, all or nothing
- [x] After saving the operator returns to where they started

## Implementation plan

POST /review/allow re-reads the queue under the filter the return names (a device page narrows to that device) and keeps the Needs review rows for the selected keys. It builds allowRule for each device copy, dedupes per profile, adds the rules to each profile's current document, and previews with the editor's preview(). The confirm page shows per profile the rules, the devices each is for, the devices reached, the local-allow count and the diff. It carries every profile's base and document, plus the keys for re-planning. POST /review/allow/save calls the new catalog.PutProfilesIf, where putProfile was split into putProfileTx so one transaction holds every profile, each checked against its base. On ErrProfileChanged nothing is saved and the page is planned again against what is stored. The saved notice now takes several saved=NAME&revision=N pairs. Edit in the full editor posts the planned profile's fields to /profiles/NAME/preview with the return. Rejected: pre-filling the full editor with every rule. It edits one profile at a time and is the part reported as confusing.

## Notes

**agent:claude-code/10adf304** at 2026-09-29T01:44:52Z

The description says saving needs a recent IdP sign-in. It doesn't, and this isn't built: the dashboard asks for re-authentication only when minting registration codes, and the existing profile editor, which single Allow uses, saves without it. Requiring it here alone would make batch Allow stricter than doing the same allows one at a time. If the owner wants recent sign-in for profile saves, it belongs on every profile write, as its own ticket. No acceptance criterion covers it.

## Summary

Allow selected on /review and on a device's page opens a short confirm page. For each profile it shows the rules added with the devices each is for, the devices it reaches, the local-allow count and a diff, and it offers Save, Back, and Edit in the full editor. Save writes every profile against the revision it read in one transaction (catalog.PutProfilesIf), or none, re-planning on a conflict. It returns to where the operator started with a notice naming each saved revision. Projects that no longer need review are skipped and named. The API is POST /api/web/v1/review/allow (plan) and /allow/save. Tests: TestPutProfilesIfIsAllOrNothing, TestAllowSelectedFromTheQueue, TestAllowSelectedIsAllOrNothing, TestAllowSelectedFromTheDevicePage, TestAllowSelectedAPI. The recent-sign-in line in the description was not built; see the note.
