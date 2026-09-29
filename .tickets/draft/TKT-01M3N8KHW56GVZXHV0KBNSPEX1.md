---
schema: 3
id: TKT-01M3N8KHW56GVZXHV0KBNSPEX1
title: "Bays: segment one lake and route sessions to a bay"
type: epic
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
  - area/catalog
  - area/agent
  - area/auth
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T00:21:33Z
updated_at: 2026-09-29T00:21:33Z
created_by:
  id: agent:claude-code/7859b064
  name: ""
updated_by:
  id: agent:claude-code/7859b064
  name: ""
extensions: {}
---

## Description

### Idea

One lake can be split into named **bays**. Each session is sent to one bay: the default bay, which every lake has, or a bay a user created. Bays let one lake hold work that should be kept apart, such as work and personal sessions, or one client's projects, without running a second lake.

The name comes from the lake metaphor. A bay is part of the lake and shares its water, so it is not a second lake. `docs/naming.md` should record the choice. "Pond" was the first word for this and was dropped because lampi already means pond. "Partition" was rejected because the repository uses it for the parquet partitions, and "space" and "collection" because they already appear in the code and docs.

### How this differs from many lakes

TKT-01M3FHHBC (Agent onboarding: registration codes, lake config, many lakes) already lets one agent send different sessions to different lakes, each with its own token, allowlist and state. That gives separation by running a second lake. Bays give separation inside one lake: one host, one store, one operator. TKT-01M3FHHBC lists "multi-tenant lakes" as out of scope, and this epic reopens part of that.

### Status

An idea, being fleshed out by a grilling session with the owner. The design decisions and open questions are recorded below as they are settled. No children have been filed yet.
