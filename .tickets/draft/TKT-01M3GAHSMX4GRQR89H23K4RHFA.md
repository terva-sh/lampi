---
schema: 3
id: TKT-01M3GAHSMX4GRQR89H23K4RHFA
title: "Lake base config: refuse zero-valued forbidden fields, show deny sources"
type: bug
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/agent
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-27T02:19:20Z
updated_at: 2026-09-27T02:19:20Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Two findings from terva-review 993 on PR #16 (TKT-01M3FHHBK, Lake base config), deferred at the owner's direction on 2026-09-27 so the onboarding stack could merge.

### Forbidden profile fields pass when their value is zero

`ParseProfile` in internal/config/profile.go decodes into `Profile`, and `Validate` refuses a harness root only when `Root != ""` and upload_hits only when `UploadHits` is true. So a profiles file that contains `"redaction":{"upload_hits":false}` or a harness `"root":""` loads, even though both fields are forbidden in a profile. The zero values change nothing at merge, since the machine's own settings win, but the file should be refused. The rule is "not allowed here", not "not allowed to be non-zero". Fix: detect that the forbidden keys are present while decoding (a raw-map pass, or pointer fields), and test the zero-value cases beside the non-zero ones.

### agent config does not say where deny rules came from

`ApplyLakeProfile` in internal/config/merge.go appends a profile's deny rules to the local ones and records only `AllowFrom`. `agent config` then prints `projects_deny` as a count, with a source for allow rules and none for deny rules. When an upload is refused, the operator cannot tell whether the rule came from config.json, the profile, or both. Fix: track where each deny rule came from and print it, including the mixed case.
