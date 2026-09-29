---
schema: 3
id: TKT-01M3NM01KV7WF9SE218PWHR2GN
title: "Profile preview: list the projects a change admits and drops"
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/server
assignees: []
milestone: null
parent: TKT-01M3NM01J4731BSWEA815FWGXS
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/58fb7d84
  branch: t3code/improve-profile-rule-matching
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-58fb7d84
  commit: 4826f0b8cbb8d0242786c802219afff34d3b240d
  session: null
  claimed_at: 2026-09-29T03:42:05Z
  expires_at: null
archive: null
created_at: 2026-09-29T03:40:36Z
updated_at: 2026-09-29T03:51:37Z
created_by:
  id: agent:claude-code/58fb7d84
  name: ""
updated_by:
  id: agent:claude-code/58fb7d84
  name: ""
extensions: {}
---

## Description

The profile editor's preview shows the line diff, the changed fields and the devices the profile reaches. It does not show what the change does to those devices' projects. A `git_remote_prefix`, or any wider rule, can admit projects nobody looked at, and the operator cannot see which ones before saving.

### Approach

For each active device that fetches the profile, read its newest inventory (`DeviceInventoryOf`) and evaluate every inventoried project under the stored profile's `projects` and under the edited one, with `config.Projects.Permitted` on a `config.ProjectID` built from the row (cwd, cwd hash, git remote). List the projects whose verdict changes, grouped by project key (`catalog.ProjectKeyOf`) across devices:

- **Admits:** refused under the stored rules, permitted under the edited rules.
- **Drops:** permitted under the stored rules, refused under the edited rules.

Each row names the key, the devices, and the session count. Put the list on the HTML preview, and on the JSON preview if the API has one.

### Limits the preview must state

- A device whose `config.json` sets its own allow rules (`allow_source` local) is not reached by profile allow rules. Leave it out and keep the existing `LocalAllow` count.
- A STRICT device sends no refused rows. Say how many devices on the profile are strict or have sent no inventory, since the list cannot speak for them.
- The lake cannot see a device's local deny rules. A row the device refused with `a deny rule matches` stays refused whatever the profile's allow rules say, so it is never listed as admitted.

### Files

- `internal/web/profile_edit.go` (`preview`, `profilePreview`)
- `internal/web/templates/page.html`
- `docs/web-dashboard.md`, and `docs/web-api.md` if the JSON shape changes

## Acceptance criteria

- [x] The preview lists each project whose verdict changes, as admitted or dropped, with its devices and session count
- [x] Devices with local allow rules are left out, and strict devices and devices with no inventory are counted
- [x] Tests cover admit, drop, local allow, strict and deny rows; docs describe the list
- [x] A deny-refused row is left out only when the device reports local deny rules, applied the stored profile, and the stored deny rules do not match it

## Implementation plan

- `reach` in `internal/web/profile_reach.go` reads the newest inventory of each device on the profile. It evaluates every row under the stored `projects` and under the edited ones with `Projects.Permitted`, and groups the rows whose verdict flips by `catalog.ProjectKeyOf`.
- `preview` calls it only when `projects.allow` or `projects.deny` changed, and the editor template renders it above the diff.
- Test: `TestPreviewListsTheProjectsAChangeAdmitsAndDrops`. Docs: the editor section of `docs/web-dashboard.md`.

## Notes

**agent:claude-code/58fb7d84** at 2026-09-29T03:46:47Z

### Decisions

- **The rows' `allowed` flag is ignored.** It reflects whichever profile version the device last applied, which may not be the stored one. The preview evaluates the raw project ID under both rule sets, so a stale inventory still gives the right difference.
- **Local deny rules are inferred.** The lake cannot see a device's own deny rules. A row refused with `a deny rule matches`, where the stored profile's deny rules do not match it, is taken as denied on the device and skipped.
- **Criterion 3 is reworded.** It said a deny-refused row is never listed as admitted. When the stored profile's own deny rule explains the refusal and the edit removes that rule, the row is listed as admitted. The device might also deny it locally, but for a warning, listing too much is the safe direction. The criterion was replaced with this rule. This note supersedes the old wording.
- **The editor preview only.** The batch Allow confirm page adds exact rules, so what it admits is the selected projects, which it already lists. TKT-01M3NM01Q (Allow: choose repository or owner width for the new rule) brings the list to that page with owner width.

### Verification

- `GOFLAGS=-mod=mod just ci` passes.
- Disabling the local-deny skip makes the new test fail.
- The page was not viewed in a browser: the preview browser cannot reach loopback on the dev host. The rendered text is asserted in the test.

**agent:claude-code/58fb7d84** at 2026-09-29T03:51:37Z

### Review 1318 on PR 131, and the local-deny rule revised

This supersedes the "Local deny rules are inferred" and "Criterion 3" points of the previous note.

- **Finding 1, high.** A deny reason the stored profile does not explain was taken as a local deny. The reason can instead come from an older profile the device still runs, which would hide exactly the widening the operator needs to see. Accepted and fixed. A row is now skipped only when all of these hold:
  - the device's `deny_source` names `local`;
  - the device applied the stored profile version;
  - the stored profile's deny rules do not match the row.
  Otherwise the row is evaluated like any other, and the preview may list a project the device still denies itself. `profileDevice` carries the device's `deny_source` and applied version, as `json:"-"` fields so the profiles API is unchanged.
- **Finding 2, low.** `.tickets/epics.md` linked the epic's draft path, because the index was regenerated before promotion. Fixed with `check --fix`.
- **Criterion 4** now states the revised rule.
