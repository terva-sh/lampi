---
schema: 3
id: TKT-01M3M7M0Y72P3YK1X47AEXM79E
title: "Push profile changes: version header and immediate agent fetch"
type: task
status: draft
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
updated_at: 2026-09-28T14:45:05Z
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

- [ ] Agents fetch a changed profile within seconds of the edit
- [ ] An agent that starts with the lake down applies its cached profile
- [ ] Heartbeats report the applied version
