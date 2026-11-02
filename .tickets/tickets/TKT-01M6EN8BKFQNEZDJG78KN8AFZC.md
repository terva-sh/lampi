---
schema: 3
id: TKT-01M6EN8BKFQNEZDJG78KN8AFZC
title: "Release v0.11.0: profile doctor, Health panel and folder rules"
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/ci
  - area/ops
  - area/docs
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/t3-43103b81
  branch: release/v0.11.0
  worktree: /home/sothr/.t3/worktrees/lampi/t3-43103b81
  commit: 7e9b646683c1008b184fefc798dec370facaac5d
  session: null
  claimed_at: 2026-11-02T17:35:09Z
  expires_at: null
archive: null
created_at: 2026-11-02T17:35:02Z
updated_at: 2026-11-02T17:37:34Z
created_by:
  id: agent:claude-code/t3-43103b81
  name: ""
updated_by:
  id: agent:claude-code/t3-43103b81
  name: ""
extensions: {}
---

## Description

Release v0.11.0 from main: everything merged since v0.10.0, PRs #231 to #236. It is a minor version, as v0.10.0 was, because it adds features. There is no schema change. The owner asked on 2026-11-02 to cut a release so it can be deployed to the lake; that authorizes the tag on both forges. Upgrading the live lake is not part of this ticket.

The release PR also fixes the `serve profiles doctor` text output, which the rehearsal caught. It printed "1 safe fix change" and ran a 16-character code into the next column.

### Release notes

This release helps keep agent profiles short and safe. A profile that grows one Allow at a time collects rules that add nothing, and rules that reach further than anyone chose. It can also reach the 500-rule cap.

**Profile doctor.** `terva-lampi serve profiles doctor NAME` reads a profile's allow rules against the newest inventory of each device that takes them, and lists what to change:
- rules for `/`, a home directory, or the folder holding home directories, which admit every project started there;
- rules for a top-level folder such as `/tmp`, or for a whole host such as `github.com`;
- rules another rule covers, and rules a deny rule always overrides;
- rules for temporary folders: under `/tmp`, `/var/folders` or a `.cache` folder, or named as `mktemp` and Go tests name folders;
- rules no project on any device matches;
- groups of rules one owner or folder rule would replace;
- how close the profile is to the cap, counting the projects waiting in review.

`--json` prints the report for a script. `--write FILE` writes the profile with the safe fixes applied, those that change no project's verdict on any device, for `serve profiles set`. The doctor itself changes nothing.

**Health panel.** The profile page and the editor show the same report. The editor's "Fewer rules" panel is replaced by Health, where each fix is a button that changes the form and previews it:
- **Apply safe fixes** removes covered and shadowed rules, and temporary rules no device holds.
- **Narrow** replaces a home, top-level or host-wide rule with one exact rule for each project it admits today. Each new rule keeps the old rule's folder or host, so it never admits what the old rule refused. A session started in the home directory itself keeps a `cwd_hash` rule, which names that folder alone.
- **Remove and hide** removes a temporary rule that a device still holds, and hides its projects from review when you save, so they do not come back.
- Owner and folder folds, and **Remove unmatched rules**.

**Folder rules.** Allow at owner width now gives a project with no git remote one `cwd_prefix` for the folder above it. The review queue and the device page offer **Allow FOLDER/…** beside **Allow OWNER/…**, so hundreds of worktrees under one folder take one rule. Neither the Allow nor the editor's new folder fold ever names a top-level folder, a home directory, or the folder holding home directories.

#### Upgrading from v0.10.0

- **Catalog:** no migration; the schema stays at 21.
- **Search index and normalizers:** unchanged.
- **Agents:** unchanged. Profiles keep the same format, and older agents keep working with this lake.
- **Dashboard:** the profile editor's `tidy=covered` form action is replaced by `doctor=safe`, which removes the same rules and more. No API changes.
- **Rehearsal:** an isolated upgrade from v0.10.0 to this release passed on synthetic data:
  - the catalog stayed at schema 21, the search index at version 8, and an unchanged sync uploaded nothing;
  - `serve profiles doctor` ran while `serve` ran, and flagged the home, covered and temporary rules;
  - `--write` followed by `serve profiles set` dropped the covered rule as revision 2;
  - an appended session uploaded, and `fsck` found 0 bad entries;
  - v0.10.0 then reopened the lake, read the profile the doctor wrote, and an unchanged sync uploaded nothing.

**Rollback** is a binary or image swap. Profiles saved with this release are valid for v0.10.0. Projects hidden by Remove and hide stay hidden, and v0.10.0 lists and unhides them as usual.

## Acceptance criteria

- [ ] An isolated v0.10.0 upgrade rehearsal and the release gates pass on the synchronized main commit
- [ ] The tag is published on both forges, and the archives, checksums and images name the tag
- [ ] Both release bodies describe the features, the upgrade and rollback

## Notes

**agent:claude-code/t3-43103b81** at 2026-11-02T17:37:34Z

Gates on release/v0.11.0 (origin/main 7e9b646 plus the doctor output fix): GOFLAGS=-mod=mod just ci passed; just release-check passed; just release-snapshot built all five archives and checksums. Isolated rehearsal (release-v0110.* under the agent-scratch lampi root, rehearse.sh and rehearse.log) with the snapshot's linux_amd64 binary: v0.10.0 archive verified against checksums.txt; v0.10.0 serve with web config and token, a synthetic Claude session synced, profile imported; candidate: migrate --check reports schema 21 up to date, index stays at 8, unchanged sync uploaded 0; serve profiles doctor ran while serve ran and flagged home_folder, covered and temporary; --write then set saved revision 2 without the covered rule; appended session uploaded; fsck 3 entries 0 bad; v0.10.0 reopened the lake, read revision 2, and an unchanged sync uploaded 0; no errors in serve logs. No live config, token, lake or agent touched.
