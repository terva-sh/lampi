---
schema: 3
id: TKT-01M6F5A01MX2ZCP9J7M3GGD7M0
title: Preview and separate Vuoro review sessions into a bay
type: spike
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
  - area/catalog
  - policy
assignees: []
milestone: null
parent: TKT-01M6F5K38C9N9Y1TRX4PZB7JSW
origin: null
dependencies:
  - TKT-01M6F5K49E9JCCE6C80FTTNRP3
  - TKT-01M6F5K429GX4TZFJB35TV3KC7
  - TKT-01M6F5K4X6SQP6Y1VXQ77JBYE0
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-11-02T22:15:33Z
updated_at: 2026-11-02T22:21:09Z
created_by:
  id: agent:codex/t3-6387cc8a
  name: ""
updated_by:
  id: agent:codex/t3-6387cc8a
  name: ""
extensions: {}
---

## Description

### Owner goal

Use lampi's own agent interfaces to inspect the local lake and separate Vuoro code reviews from project-development sessions. The owner identified the newly ingested reviews as Vuoro running Claude Code. This is session grouping into a bay; creating a second capture profile is not required merely to separate already captured sessions.

### Verified evidence

On 2026-11-02, all six existing MCP recall tools succeeded against the local lake. Search and read_events identify review sessions under /home/sothr/.local/state/vuoro/worktrees/<timestamp-id>. Sample reviewer sessions 01M6F532E20X2S9YFJQP9ZR4TR and 01M6F4Z4BV9SWF5TXE375T7XQ0 begin with a prompt to review local commits. Companion 01M6F55Q2DJCFV57PGW0WFE87W begins with a prompt to check findings and mark them with rate_draft, has no project_id, and shares the first reviewer's working-directory hash. Include both roles in a code-review bay. SDK origin and harness=claude alone also describe other sessions and are not sufficient selectors.

The current agent consumes the default capture profile with 75 allow rules. Its cached profile already permits /home/sothr/.local/state/vuoro/worktrees. Other existing allow rules mention Vuoro paths on macOS, but their review purpose and current captured sessions have not been verified. Do not extrapolate one machine's directory layout to the whole lake.

### Proposed investigation and migration

First obtain permitted metadata access to the current bay list, memberships, routing rules and working-directory evidence. The current events:read MCP surface cannot enumerate these; local lake data is protected by its service account. TKT-01M6F56Z6QP6C2P3M1T7J923BV (MCP and CLI: discover bays, projects and session metadata) records that tooling gap.

Evaluate code-reviews as the target bay name and the verified Vuoro review working-directory prefix plus harness=claude as an initial selector. Check whether this folder holds only review workflows, enumerate other machines/layouts explicitly, and inspect controls before accepting that selector. Confirm intended readers because a bay is also an access boundary.

For an operator with lake-host access, the existing CLI supports creating the target bay, adding an --add rule with --cwd-prefix and --harness claude, and moving existing sessions from default with the same filters and --dry-run first. Substitute only verified selectors. Adding a rule affects subsequent manifests; apply-rules only adds membership to existing sessions, so neither alone removes reviews from default. A move into a bay requires that bay to exist even in dry-run. A session-specific selection cannot currently be passed as a bulk UID list, and purpose matching is not implemented.

Use the existing metadata routing when validated. If review and development sessions share all available match fields, design explicit provenance/classification and a selection preview before attempting that subset. Leave the capture allowlist alone unless an independent capture-policy change is requested. Preserve session identities and artifacts; do not re-register devices or re-import sessions to regroup them.

### Limits and alternatives

No live bays, memberships, grants, routing rules or profiles were changed. No complete move preview has been run. Searching for review or vuoro returned continuing pages and is not an exact inventory. Routing an entire repository or all Claude sessions would also capture development work. A separate capture profile would not reclassify stored sessions. Store only metadata and decisions in this ticket, never token values or raw transcript dumps.

## Acceptance criteria

- [ ] Inventory current bay membership and enumerate candidate review sessions, including finding-rating companions and sessions without project IDs.
- [ ] Validate directory selectors against candidate prompts and representative development sessions, and produce a complete dry-run move list with exclusions.
- [ ] Specify future routing and existing-session moves separately, including required reader grants and behavior on later manifests.
- [ ] Record the approved split or the precise tooling still needed, without claiming a sampled search is a complete migration preview.
- [ ] Exercise the new discovery/selection tools and doctor recommendations on the Vuoro case, recording false positives, companion coverage and any remaining UX/tool gaps.
- [ ] Review add versus move and one versus multiple destinations explicitly, verify future manifests do not unexpectedly restore source memberships, and record any operator-approved live changes separately.

## Notes

**agent:codex/t3-6387cc8a** at 2026-11-02T22:20:33Z

The owner expanded the planning scope to multi-bay UX, agent/CLI tools and doctor heuristics. This ticket remains the real-world validation exercise under TKT-01M6F5K38C9N9Y1TRX4PZB7JSW (Discover sessions and organize them into one or more bays). Its original proposed single review bay is an initial case, not a one-destination restriction. Validate reviewer/rater cohorts and missing project IDs using TKT-01M6F56Z6QP6C2P3M1T7J923BV (MCP and CLI: discover bays, projects and session metadata); consume the shared plan from TKT-01M6F5K45WCQKE37JN4CC0WEY7 (Bays: plan and apply selected sessions to multiple destinations) and recommendations from TKT-01M6F5K4X6SQP6Y1VXQ77JBYE0 (Doctor: suggest bay destinations with supporting evidence). Do not treat a text-search page or a heuristic suggestion as authorization to migrate the live lake. The new backlog can be implemented and tested on synthetic data before deployment; a deployment prerequisite and live operator decision must be explicit when this exercise is picked up.
