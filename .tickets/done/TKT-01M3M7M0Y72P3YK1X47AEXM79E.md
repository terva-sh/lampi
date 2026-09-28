---
schema: 3
id: TKT-01M3M7M0Y72P3YK1X47AEXM79E
title: "Push profile changes: version header and immediate agent fetch"
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area/protocol
  - area/agent
assignees: []
milestone: null
parent: TKT-01M3M7KB32E2BCFEA9CN710522
origin: null
dependencies:
  - TKT-01M3M7M0RQKWEVAZX1K6RD270P
  - TKT-01M3M7M0WCZQB2ETXX1PNKHRBY
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T14:45:05Z
updated_at: 2026-09-28T17:21:40Z
created_by:
  id: agent:claude-code/2cf53976
  name: ""
updated_by:
  id: agent:claude-code/2cf53976
  name: ""
extensions: {}
---

## Description

An edit to a profile reaches the agents using it within seconds. Today `watchProfile` fetches at start and then every hour (`profileEvery`).

- The lake returns the current version of the device's resolved profile in a response header on every authenticated agent request (blob checks, manifests, heartbeats). The agent compares it with its cached version and fetches `GET /v1/agent/config` as soon as they differ. The fetch keeps its current verification against the pinned key, lake id and device id.
- An idle agent sends a heartbeat every few minutes, so it sees the header then. No long-lived connection is needed.
- The hourly fetch stays as a backstop.
- Durability: the cached copy is already written with `WriteFileAtomic` and read at start. Add a test in which the lake is down when the agent starts: the agent must apply the cached profile and upload under it, and must not fall back to the local rules alone.
- The heartbeat reports the applied version, so the dashboard can show "3 of 4 devices on the current version" and name the stale one.

### Alternatives considered

- Shorter polling: simple, but it scales requests with devices times frequency and still lags.
- Long-poll or SSE: the fastest, but it adds a long-lived connection through reverse proxies, when the header gives seconds of latency for no extra requests.

## Acceptance criteria

- [x] Agents fetch a changed profile within seconds of the edit
- [x] An agent that starts with the lake down applies its cached profile
- [x] Heartbeats report the applied version

## Implementation plan

One PR.

- **Lake:** `authed` resolves the calling device's profile and sets a `Lampi-Profile-Version` header on every authenticated answer. A lake with no device tokens names the default profile's version. A lake with no identity sends no header.
- **Client:** `upload.Options.ProfileVersion` receives the header from `doRequest`.
- **Agent:** each lake runner stores the newest version the lake named and nudges `watchProfile`. When that version differs from the cache, `watchProfile` fetches at once, but at most once every 10 seconds. A nudge that arrives inside the gap waits on a single deferred timer. The hourly fetch stays as a backstop.
- **Idle latency:** idle agents report every minute instead of every 5, so an idle agent sees an edit within about a minute.
- **Durability test:** an agent starts with the lake down and uploads under its cached profile.
- The heartbeat already reports the applied version (TKT-01M3M7M0RQ).

## Notes

**agent:claude-code/2cf53976** at 2026-09-28T17:21:40Z

Resolving the profile costs one indexed read per authenticated request, on top of the device read that already happens. Caching versions in memory was rejected: 'serve profiles set' runs as a separate process and writes the catalog directly, so serve could not invalidate such a cache. The same PR fixes a heartbeat bug found during the self-update survey: agent_report.go sent the raw ldflags version, which goreleaser never sets, so every release agent reported 0.0.0. releaseVersion() is now the single helper; it reads the ldflags value and falls back to build info.

## Summary

Every authenticated answer from the lake names the device's current profile version in Lampi-Profile-Version. An agent whose cache differs fetches the signed profile at once, at most every 10 seconds. A syncing agent sees an edit within seconds, and an idle one within a minute, because reports now go out every minute. The hourly fetch remains as a backstop. A test covers an agent that starts with the lake down and uploads under its cached profile. The same PR fixes release agents reporting version 0.0.0.
