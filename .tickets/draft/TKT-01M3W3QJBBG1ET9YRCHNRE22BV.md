---
schema: 3
id: TKT-01M3W3QJBBG1ET9YRCHNRE22BV
title: "CLI: terva-lampi doctor checks a machine's lampi setup"
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/agent
  - area/ops
assignees: []
milestone: null
parent: TKT-01M3W3PZSDCB5FGKJS06MSZH65
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-01T16:11:02Z
updated_at: 2026-10-01T16:11:02Z
created_by:
  id: agent:claude-code/0f3154cf
  name: ""
updated_by:
  id: agent:claude-code/0f3154cf
  name: ""
extensions: {}
---

## Description

When lampi does not run correctly, the person has to know which of
`status`, `agent config`, `systemctl --user status` or `launchctl
print`, and the logs to read. Add `terva-lampi doctor`, which checks
a machine's setup and says, for each problem, what is wrong and the
command that fixes it.

Agent side:

- the user service is installed, enabled and running, and its binary
  path is the binary that runs `doctor`;
- each lake's server URL and token file, with their source, and
  whether the lake answers `/healthz`;
- a lake with no pinned key (pointing at `lakes adopt`, never
  `register`);
- the token file's mode, and harness roots that do not exist.

Lake side, with `serve doctor` or a flag:

- the data directory is on a local filesystem, not NFS or SMB;
- `identity.json` exists and is readable only by its owner;
- `--web-config` validates, and its issuer's discovery answers;
- the clock is in sync, since codes and OIDC check times.

`doctor` reads and reports. It changes nothing.

## Acceptance criteria

- [ ] doctor checks the agent service, each lake's URL, token, health and pinned key
- [ ] A lake-side check covers the data filesystem, identity.json, web config and clock
- [ ] Every finding names the command that fixes it, and doctor changes nothing
