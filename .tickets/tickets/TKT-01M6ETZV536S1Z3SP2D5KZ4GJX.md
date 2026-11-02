---
schema: 3
id: TKT-01M6ETZV536S1Z3SP2D5KZ4GJX
title: "Profile editor: one line per rule, matches, inline findings, filter"
type: task
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/server
  - policy
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/t3-43103b81
  branch: t3/profile-editor-redesign
  worktree: /home/sothr/.t3/worktrees/lampi/t3-43103b81
  commit: 98d247e49c147d3b0f5eb39b38e3e72a95a82f25
  session: null
  claimed_at: 2026-11-02T19:15:15Z
  expires_at: null
archive: null
created_at: 2026-11-02T19:15:14Z
updated_at: 2026-11-02T19:34:41Z
created_by:
  id: agent:claude-code/t3-43103b81
  name: ""
updated_by:
  id: agent:claude-code/t3-43103b81
  name: ""
extensions: {}
---

## Description

The owner, 2026-11-02: "The editor view is actually kinda hard to use … it doesn't take advantage of the page width when there are a lot of fields to edit. I think the whole view could use a redesign toward usage now that we have a large sample size to work from." The live default profile holds 234 allow rules. Each is a row of five inputs (cwd_prefix, git_remote, git_remote_prefix, cwd_hash, cwd_glob), though almost every rule sets one field.

The owner chose (AskUserQuestion, 2026-11-02): one line per rule, matches per rule, Health findings inline, and a filter box. Ordering (TKT for ordering) and select-and-merge (TKT-01M6ETH6H726EM1DJJS98M56JV) follow in their own PRs, so each stays reviewable.

### What changes

- Each rule is one line: a kind picker (folder, repository, owner, folder hash, folder pattern) and one value box across the page width. A rule with more than one field shows each field as its own condition, and "Add a condition" adds one. The form posts `allow.N.c.M.kind` and `allow.N.c.M.value`; the old `allow.N.FIELD` names stay accepted.
- Each rule shows what it admits on the listed devices: projects and sessions, from the doctor's `admitted`. A rule that admits nothing says so.
- The doctor's findings for a rule appear on that rule, with the fix it offers. The Health panel stays as the summary.
- Rules that share a folder or owner are drawn under a header naming it and the count.
- A filter box narrows the rule list by text where scripts run. Without scripts every rule shows.

## Acceptance criteria

- [x] Each rule is one line with a kind picker and a full-width value; multi-field rules show each condition; old field names still parse
- [x] Each rule shows the projects and sessions it admits, and the doctor's findings for it
- [x] Rules sharing a folder or owner sit under a header; a filter box narrows the list where scripts run
- [x] Tests and docs cover the new form

## Notes

**agent:claude-code/t3-43103b81** at 2026-11-02T19:23:54Z

Built on t3/profile-editor-redesign, stacked on #241. Rules post as list.N.c.M.kind and .value, and readRule still reads list.N.FIELD, which the Health fix forms (profile-hidden-form) and older tests post. A rule the form cannot read no longer empties the editor: readRules reads every row, and readProfileForm keeps the note, harnesses and hides. profiledoctor.Report gains Uses, which the CLI's --json also prints. Remove posts remove=list.N. Group headers are computed from runs in stored order, so the ordering step (TKT-01M6ETZV7C0P5F2RDKQPAQCCFW) only has to reorder. I checked the layout from a headless Chromium screenshot of a rendered page, and compacted the rows after the first one. GOFLAGS=-mod=mod just ci passes.

**agent:claude-code/t3-43103b81** at 2026-11-02T19:29:13Z

terva-review on #242 (run 2257), two findings, both fixed. (1) A rule the form could not read came back rebuilt from the partial ProjectMatch, which lost the conflicting value and everything after it. readRule now also returns every condition as sent; readRules keeps them per row (profileForm.AllowRaw/DenyRaw), and ruleRows shows them. (2) The doctor reads normalizeRules(form), which drops empty rows, but ruleRows indexed by form row, so a rule after an empty or removed row got another rule's use and findings. ruleRows now counts each row's normalized index. Tests: TestRuleSentAsIs, the after-an-empty-row case in TestRuleRows, and a check in TestEditorRuleLines.

**agent:claude-code/t3-43103b81** at 2026-11-02T19:34:41Z

terva-review on #242 (run 2265): Remove and the inline fixes came before Preview in the editor form, so Enter in a value box submitted the first rule's Remove. Fixed with an off-screen, untabbable Preview button first in the form. It is not display:none, which some browsers skip for implicit submission. TestEditorRuleLines checks that the first submit button carries no name. The run before it (2263) flagged filtered rows staying visible under display:grid. That was not real: lake.css has a global [hidden]{display:none!important}, which I said on the PR.
