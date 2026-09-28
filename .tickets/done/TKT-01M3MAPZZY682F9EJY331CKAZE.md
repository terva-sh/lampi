---
schema: 3
id: TKT-01M3MAPZZY682F9EJY331CKAZE
title: "Devices: show agent version, flag outdated and known-bad agents"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/agent
  - area/protocol
assignees: []
milestone: v0.2.0
parent: TKT-01M3MAV1XM089JPDCG5NH2RAJ6
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T15:39:08Z
updated_at: 2026-09-28T19:32:52Z
created_by:
  id: agent:claude-code/03b82158
  name: ""
updated_by:
  id: agent:claude-code/2cf53976
  name: ""
extensions: {}
---

## Description

Operators cannot see which agent build each device runs. Show the agent version per device, recommend an upgrade when a device is behind the server, and raise an urgent notice when the version is one the server knows to be incompatible or buggy.

### What exists today

On `main`, the agent does not report its version. `HelloRequest` carries only a nonce (`internal/protocol/protocol.go`), and uploads send a bare `User-Agent: terva-lampi` (`internal/upload/upload.go`). The version is stamped into `internal/cli.version` by `just build` and goreleaser; unstamped builds read `0.0.0`.

TKT-01M3M7M0RQ, Agent heartbeat: durable last contact, sync counters, applied profile, is in review in PRs #66 and #67. It adds `POST /v1/agent/report`, which carries the agent version among other fields, and a `device_reports` table that keeps each device's newest report. This ticket reads the version from that report and does not add a second reporting path. Start it after those PRs merge. The heartbeat ticket is not on `main` yet, so this is written here rather than as a `depends-on` link. Add the link once it lands.

### Shape

1. **Read.** Take each device's agent version, and the commit when present, from its newest heartbeat report. A device with no report shows as "unknown", not as outdated. Also send the version in the User-Agent (`terva-lampi/0.1.3`) so request logs show it.
2. **Compare.** A device is *behind* when its version is lower than the server's own build version, compared as semver. `0.0.0` and dev builds are "unstamped" and are neither flagged nor compared.
3. **Advisories.** The server binary embeds a mapping, for example `internal/advisory/agents.json`, of version ranges to a severity (`upgrade`, `urgent`) and a short reason that links to the release note or ticket. A device that matches an `urgent` entry gets a prominent notice. The same notice can cover a "too far behind" rule, such as more than N minor versions behind or a protocol version the server plans to drop. Because the mapping ships with the server, upgrading the lake is enough to warn about agents already in the field.
4. **Surface.** Show version, a behind/urgent badge and the advisory reason in the devices list of the operations page (TKT-01M3JV45Z, done) and in the device-management page (TKT-01M3J5HXA). Put a banner at the top of the dashboard when any device is urgent. Optionally export a `lampi_device_agent_outdated{severity}` gauge on the metrics listener so it can alert.

### Open questions

- Should the lake tell the agent about a matching advisory, in the `/v1/agent/report` answer or on `/v1/hello`, so `terva-lampi sync` or `status` prints "this agent is known-bad, upgrade" on the machine itself? This is cheap once the mapping exists, and it reaches people who never open the dashboard.
- Should the server ever refuse a known-incompatible agent? The default here is to warn only. Refusal belongs to protocol versioning, not to this mapping.
- Is "behind the server" the right baseline? It assumes agents and the lake release together, and a newer agent than the server is also worth a quiet note.

## Acceptance criteria

- [x] Devices views show version with a behind badge versus the server build
- [x] Embedded advisory mapping raises an urgent notice for matching versions, with a reason
- [x] Tests cover semver comparison, unstamped builds and advisory range matching
- [x] Devices views read the version from the newest heartbeat report; no report shows as unknown
- [x] Agent sends its version in the User-Agent

## Implementation plan

D1 shows each agent's reported release against the lake's release (behind/current/ahead/unstamped/unknown via internal/release) with a behind count, and the upgrade command in the page notes. D3 adds the known-bad mapping (internal/advisory/agents.json) and an urgent banner, and puts the release in the agent User-Agent. Version comes from the heartbeat report, not the User-Agent, because the report is already stored durably and survives a serve restart.

## Notes

**agent:claude-code/2cf53976** at 2026-09-28T17:21:40Z

Correction from TKT-01M3M7M0Y7: goreleaser does not stamp internal/cli.version (.goreleaser.yaml links nothing in with -X). Releases get their version from build info. The heartbeat therefore reported 0.0.0 from every release agent until releaseVersion() in internal/cli/cli.go was added. Use releaseVersion() for the User-Agent as well.

**agent:claude-code/2cf53976** at 2026-09-28T18:35:00Z

D3 on web/agent-advisories (stacked on web/device-actions):

- **Advisory file.** `internal/advisory` embeds `agents.json`, a list of `{introduced, fixed?, severity upgrade|urgent, reason, link?}`, in the OSV style of introduced and fixed.
  - Parsing is strict: unknown fields, versions that are not releases, `fixed` not after `introduced`, a missing reason, or a link that is not https are all refused.
  - `Match` prefers an urgent advisory. Unstamped builds match nothing.
- **Dashboard.** Each device row gets a severity badge, the reason, the fixed release and the link. `devicesView.urgent` names active devices on an urgent release.
  - A banner appears on the overview and on /devices.
  - Revoked devices are left out of the banner because they no longer upload.
- **User-Agent.** The agent sends `terva-lampi/VERSION`, or `dev` when the build has no plain version token. It is set in an init in cli from releaseVersion(). `upload.UserAgent` is a package variable because about six call sites build `upload.Options`.
- **The file ships empty.** v0.1.3 is the newest tag and predates the heartbeat, so no field agent can report a version to match against. The first real entry belongs in the release that fixes a bad one. docs/development.md says how to add one.

Not done:

- The optional `lampi_device_agent_outdated` gauge.
- Telling the agent itself about a matching advisory, one of the ticket's open questions. The report answer is the natural place for it, but that is a protocol change for another ticket.

Alternatives considered:

- **A Server field for the Set instead of a package variable.** Rejected: `New()` returns an `http.Handler` and would need another parameter only for tests.
- **Semver range strings (`>=a <b`).** Rejected: they need a parser and allow unions that nothing needs; introduced/fixed is what OSV uses.
- **A banner on every page.** Rejected: it would read the devices on every request. The overview and /devices are where an operator looks.

## Summary

Merged in #85 and #90. Devices show the agent's release with a behind/current/ahead/unstamped/unknown badge. internal/advisory/agents.json (introduced/fixed ranges, upgrade|urgent, reason, link) is embedded. A match shows beside the device, and an urgent one puts a banner on the overview and /devices. Agents send User-Agent terva-lampi/VERSION. The file ships empty, since no released agent reports its version yet. Not done: the optional gauge, and telling the agent itself about an advisory.
