---
schema: 3
id: TKT-01M6F5B6DCJEH3XPX3SZVPHY9X
title: "Read token form: say what each Reads choice is for, one line each"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/auth
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/t3-43103b81
  branch: t3/read-token-reads
  worktree: /home/sothr/.t3/worktrees/lampi/t3-43103b81
  commit: 9e0ffba4d95d30874714e129714c58106887fde9
  session: null
  claimed_at: 2026-11-02T22:16:12Z
  expires_at: null
archive: null
created_at: 2026-11-02T22:16:12Z
updated_at: 2026-11-02T22:19:59Z
created_by:
  id: agent:claude-code/t3-43103b81
  name: ""
updated_by:
  id: agent:claude-code/t3-43103b81
  name: ""
extensions: {}
---

## Description

The owner found the "Reads" choice on the Mint a read token form confusing, and picked by trial and error (screenshot, 2026-11-02). Two causes:

- Layout: `.filters label` stacks vertically, so each checkbox sits above its label and the permission name below it; the boxes read as belonging to the text above or below them.
- Wording: the choices name data formats ("raw artifacts", "normalized events"), not what a token is for. `events:read` is what `terva-lampi query`, `terva-lampi mcp` and agent recall need; `raw:read` serves only `/api/raw/v1` artifact downloads, which no terva-lampi command uses.

### Approach

Put the choices on their own full-width row, each a checkbox beside a purpose-first title ("Agent recall: search, query and MCP" / "Raw files: download uploaded files as stored"), the permission name in mono, and one line saying what uses it. Keep no box checked on a fresh form: TKT-01M3FPWCH chose that so the admin states the token's purpose. Reword the invalid_perms problem to match.

### Alternatives

- Check events:read by default. Not chosen: it reverses a deliberate least-privilege choice, and clear labels fix the trial-and-error without it.
- A single select of presets. Not chosen: both permissions together is a valid token, and a select hides that.

## Acceptance criteria

- [ ] Each Reads choice is a checkbox on the same line as its title, with the permission name and what uses it
- [ ] The titles lead with purpose: agent recall (events:read) and raw file downloads (raw:read)
- [ ] No box is checked on a fresh form, and a test pins the wording and layout markup

## Implementation plan

readTokenPerm gains Title and Use from readTokenPermUses; the form lists events:read first, then raw:read, as a full-width fieldset.token-reads below the fields, each a label.token-read row (checkbox, bold title, mono permission, one-line use). Fresh form still ticks nothing. invalid_perms reworded. Docs: web-dashboard.md table gains the form choice and its users; reading-the-lake.md names the new choice.
