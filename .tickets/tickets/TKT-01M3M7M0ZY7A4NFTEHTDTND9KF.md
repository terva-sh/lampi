---
schema: 3
id: TKT-01M3M7M0ZY7A4NFTEHTDTND9KF
title: "Dashboard: view, add, edit and remove agent profiles"
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/server
  - area/auth
assignees: []
milestone: null
parent: TKT-01M3M7KB32E2BCFEA9CN710522
origin: null
dependencies:
  - TKT-01M3M7M0WCZQB2ETXX1PNKHRBY
blocks_on: none
references: []
claim:
  actor: agent:claude-code/2cf53976
  branch: web/profile-editor
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-2cf53976
  commit: e0666513ffdb972d70eeaec6f0d6dfc684823e04
  session: null
  claimed_at: 2026-09-28T19:42:59Z
  expires_at: null
archive: null
created_at: 2026-09-28T14:45:05Z
updated_at: 2026-09-28T19:47:06Z
created_by:
  id: agent:claude-code/2cf53976
  name: ""
updated_by:
  id: agent:claude-code/2cf53976
  name: ""
extensions: {}
---

## Description

Operators view, add, edit and remove profiles from the dashboard, covering every field a profile can carry.

- List: name, version, updated at and by, and the devices that use it.
- Editor fields:
  - `projects.allow` and `projects.deny` rules with every field of `ProjectMatch` (`cwd_prefix`, `cwd_hash`, `git_remote`, `git_remote_prefix`). A rule may combine fields, and every field set must match.
  - Harness enable toggles.
  - `agent.debounce` and `agent.debounce_max`.
  - Anything else `Profile` allows at the time. Fields the agent refuses (`harnesses.*.root`, `redaction.upload_hits`) are not offered.
- Show `git_remote` in the normalized form `NormalizeRemote` produces, and normalize input on save, so a rule typed as `git@host:org/repo` matches.
- Before saving, show the diff and the devices it will reach. For devices that report `allow_source=local`, warn that the change will not affect their allow list.
- Operator role only; CSRF-checked POSTs; the audit log carries the OIDC actor and the diff. Viewers see profiles read-only.
- Rolling back to an earlier revision is a single action.
- Leave room in the layout for a device-override layer (for example a tab), without building it yet.

## Acceptance criteria

- [ ] Operators edit every profile field, including all ProjectMatch fields
- [ ] Saves show a diff and the devices reached, and are audited with the OIDC actor
- [ ] A revision can be rolled back

## Implementation plan

Three PRs, each small enough for terva-review, landing in order.

### P1 (web/profile-editor): read-only profiles for viewers

- `/profiles` lists every profile: name, version, revision, updated at and by, and the count of active devices that use it.
- `/profiles/{name}` shows the fields as a structured view (allow and deny rules with every ProjectMatch field, harness toggles, debounce), the devices it reaches with their allow_source (warning on `local`), and the revision history.
- JSON at `GET /api/web/v1/profiles` and `/api/web/v1/profiles/{name}`.
- A banner on both pages while a profiles.json sits in the lake directory or `--profiles` is set. serve passes the paths it warns about through Operations.
- The detail page has a tab row, "Profile" and a disabled "Device overrides", which leaves the layout room the ticket asks for.

### P2: operator editing

- Structured form with no JS: every existing rule row plus three blank ones per list. A rule with every field cleared is dropped. Harness toggles are three-way: not set, enabled, disabled. Debounce fields are text.
- Preview step: POST `/profiles/{name}/preview` validates with `config.ParseProfile` and normalizes git_remote and git_remote_prefix with NormalizeRemote. It then shows a line diff of the indented JSON and the devices reached, with the local-allow warning. The page carries the proposed document, the base revision and a note field into Save.
- Save: POST `/profiles/{name}` via a new `catalog.PutProfileIf(base revision)`, which refuses with ErrProfileChanged when another save landed first, so two operators cannot overwrite each other.
- The audit detail gains `changed=<top-level paths>`. The full diff is recoverable from the two revision documents, and it stays out of audit.jsonl to keep lines bounded.
- New profiles and delete come in the same PR. Delete keeps the catalog's refusal of profiles in use.
- JSON API mirrors: `PUT /api/web/v1/profiles/{name}` with `{document, base_revision, note}`, and `DELETE`.

### P3: rollback

POST `/profiles/{name}/rollback/{revision}` re-saves that revision's document through PutProfileIf with a note naming the revision, as one action from the history table.

### Alternatives considered

- **A raw JSON textarea.** Rejected as the main editor, because the ticket asks for every field as a form field and for normalization on save. It may come later as an "advanced" view.
- **JS-driven add and remove of rule rows.** Rejected: the dashboard has no JS framework, and forms work without it.
- **Storing the diff in the audit line.** Rejected: unbounded size, and the revisions already hold both documents.

## Notes

**agent:claude-code/2cf53976** at 2026-09-28T15:31:59Z

From TKT-01M3M7M0WC: the profiles page must show a banner while a profiles.json sits in the lake directory, or --profiles is set, saying the file is not in force and pointing to 'serve profiles import'. serve already prints the same warning at start and on SIGHUP (warnProfilesFile in internal/cli/profiles.go).

**agent:claude-code/2cf53976** at 2026-09-28T19:47:06Z

P1 on web/profile-editor: read-only /profiles and /profiles/{name} with JSON at /api/web/v1/profiles[/{name}]. Adds catalog.Profile(name). serve passes Operations.IgnoredProfiles, which re-stats on each call so the banner clears once profiles.json is moved away. warnProfilesFile now uses the same ignoredProfilesFiles list, so stderr and the dashboard cannot disagree. The Devices page's profile column links to the profile. A tab row with a disabled 'Device overrides' entry reserves the layout.
