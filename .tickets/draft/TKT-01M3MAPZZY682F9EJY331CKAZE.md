---
schema: 3
id: TKT-01M3MAPZZY682F9EJY331CKAZE
title: "Devices: show agent version, flag outdated and known-bad agents"
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area/server
  - area/agent
  - area/protocol
assignees: []
milestone: null
parent: TKT-01M3MAV1XM089JPDCG5NH2RAJ6
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T15:39:08Z
updated_at: 2026-09-28T15:41:21Z
created_by:
  id: agent:claude-code/03b82158
  name: ""
updated_by:
  id: agent:claude-code/03b82158
  name: ""
extensions: {}
---

## Description

Operators cannot see which agent build each device runs. Show the agent version per device, recommend an upgrade when a device is behind the server, and raise an urgent notice when the version is one the server knows to be incompatible or buggy.

### What exists today

The agent does not report its version. `HelloRequest` carries only a nonce (`internal/protocol/protocol.go`), and uploads send a bare `User-Agent: terva-lampi` (`internal/upload/upload.go`). The version is stamped into `internal/cli.version` by `just build` and goreleaser; unstamped builds read `0.0.0`. So the first step is reporting, before any page can show it.

### Shape

1. **Report.** The agent sends its version, and the commit when stamped, on `/v1/hello`: a new optional `HelloRequest` field, so older servers ignore it. It also sends it in the User-Agent (`terva-lampi/0.1.3`) so request logs show it. The server records the last-seen version and time per device id. A device that has never said hello with a version shows as "unknown", not as outdated.
2. **Compare.** A device is *behind* when its version is lower than the server's own build version, compared as semver. `0.0.0` and dev builds are "unstamped" and are neither flagged nor compared.
3. **Advisories.** The server binary embeds a mapping, for example `internal/advisory/agents.json`, of version ranges to a severity (`upgrade`, `urgent`) and a short reason that links to the release note or ticket. A device that matches an `urgent` entry gets a prominent notice. The same notice can cover a "too far behind" rule, such as more than N minor versions behind or a protocol version the server plans to drop. Because the mapping ships with the server, upgrading the lake is enough to warn about agents already in the field.
4. **Surface.** Show version, a behind/urgent badge and the advisory reason in the devices list of the operations page (TKT-01M3JV45Z, done) and in the device-management page (TKT-01M3J5HXA). Put a banner at the top of the dashboard when any device is urgent. Optionally export a `lampi_device_agent_outdated{severity}` gauge on the metrics listener so it can alert.

### Open questions

- Should `/v1/hello` also return the advisory to the agent, so `terva-lampi sync` or `status` prints "this agent is known-bad, upgrade" on the machine itself? This is cheap once the mapping exists, and it reaches people who never open the dashboard.
- Should the server ever refuse a known-incompatible agent? The default here is to warn only. Refusal belongs to protocol versioning, not to this mapping.
- Is "behind the server" the right baseline? It assumes agents and the lake release together, and a newer agent than the server is also worth a quiet note.

## Acceptance criteria

- [ ] Agent reports its version on /v1/hello and in the User-Agent; older servers unaffected
- [ ] Server records last-seen agent version per device; unknown and unstamped shown as such
- [ ] Devices views show version with a behind badge versus the server build
- [ ] Embedded advisory mapping raises an urgent notice for matching versions, with a reason
- [ ] Tests cover semver comparison, unstamped builds and advisory range matching
