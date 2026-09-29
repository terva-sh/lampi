---
schema: 3
id: TKT-01M3N8F354GDK9D0CTQ2BMZBMY
title: "Project review queue: hide, batch allow, short confirm"
type: epic
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/catalog
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T00:19:07Z
updated_at: 2026-09-29T01:47:35Z
created_by:
  id: agent:claude-code/10adf304
  name: ""
updated_by:
  id: agent:claude-code/10adf304
  name: ""
extensions: {}
---

## Description

Onboarding a machine's projects into the lake is slow. Each refused project is allowed one at a time from `/devices/{id}`. **Allow…** opens the full profile editor, where the new rule sits in a table of raw rule fields. Saving lands on the profile page, with no way back to the device. There is no multi-select. Nothing separates a project the operator has never looked at from one they looked at and chose to leave out. There is also no single place listing what still needs a decision. Reported by Drew Short on 2026-09-28 after onboarding several devices.

This epic adds a lake-wide review queue with hide, batch allow, and a short confirm step. It also fixes navigation on today's single Allow.

### Owner decisions (2026-09-28, Drew Short)

- **Hide is a dashboard state only.** It records that the project was reviewed and will not be imported. It sends nothing to agents and can be undone. A deny rule stays a separate edit to the profile. Rejected: hide also writes a deny rule. That turns a triage click into a policy change reaching every device on the profile, and the change can't be undone without another profile save.
- **A hide is lake-wide per project.** It is keyed the way `allowRule` keys an allow rule: the folded git remote, or the cwd when there is no remote. Hiding a repository once hides it on every device. Rejected: per-device hides. The same repository would come back for review on every new machine, which is the slowness being fixed.
- **"Newly discovered" means not yet reviewed.** The lake records when it first saw each project on each device. A refused project that is neither hidden nor covered by an allow rule stays in the review queue, newest first, until someone acts on it. Rejected: a fixed window such as first seen in the last 7 days. It lets unreviewed projects drop out of view without a decision.
- **Batch allow confirms on a short page, not the full profile editor.** The page lists the rules added to each affected profile and the devices each profile reaches. It has Save and Back, and returns to where the operator started. The full editor stays one link away. Rejected: pre-filling the editor with every rule. The editor is the part reported as confusing, and it edits one profile at a time.

### Scope notes

- A strict-mode device names no refused project, so its refused sessions show only as a count on the review page, as they do on its device page.
- Allowing for one device alone waits for per-device overrides (TKT-01M3N22HC, Per-device overrides on top of agent profiles). Batch allow targets each device's profile, as single Allow does today.

## Acceptance criteria

- [x] One page lists every refused project across devices that still needs a decision
- [x] An operator hides projects they will not import, lake-wide, and can unhide them
- [x] An operator allows several projects in one confirm and save, and returns to where they started
- [x] Single Allow returns to the device page

## Summary

Delivered in five PRs. #123 (TKT-01M3N8FHQ): single Allow returns to the device page with Back, plus a catalog-backed saved notice. #124 (TKT-01M3N8FHS): migration 16 with project sightings, lake-wide hidden projects, and ReviewQueue. #125 (TKT-01M3N8FHV): /review and its API, grouped lake-wide, with needs review, allow pending, denied, hidden and strict sections and a header count. Then TKT-01M3N8FHX: hide and unhide, singly or in bulk, from the queue and the device page. Then TKT-01M3N8FHZ: Allow selected with a short per-profile confirm page, saved all or nothing through catalog.PutProfilesIf. Not built: a recent IdP sign-in for batch saves, since no profile save requires one today (see the TKT-01M3N8FHZ note). Per-device targets wait on TKT-01M3N22HC.
