---
schema: 3
id: TKT-01M4ESZRAJ5AG6ZCMV8162WDEV
title: Give Lampi a pond icon in browser tabs and shortcuts
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-08T22:26:19Z
updated_at: 2026-10-08T22:30:36Z
created_by:
  id: agent:codex/t3code-8afe4a1e
  name: ""
updated_by:
  id: agent:codex/t3code-8afe4a1e
  name: ""
extensions: {}
---

## Description

Lampi currently appears as a generic icon in browser sidebars. Add a recognizable pond mark based on the dashboard waves and palette, with favicon discovery and saved-shortcut support.

## Acceptance criteria

- [x] Browser pages declare a recognizable pond favicon.
- [x] Unauthenticated favicon discovery works without an OIDC redirect.
- [x] Vector and raster icons remain legible at tab and shortcut sizes.

## Implementation plan

Create a native SVG pond mark using the existing blue and cream palette, and rasterize it for ICO and Apple touch shortcuts. Serve root discovery files publicly and declare the formats in the shared page head. Keep the dashboard wave branding and avoid adding a frontend build or image dependency. Check small-size rendering and existing web coverage; complete Forgejo CI and review before merging.

## Notes

**agent:codex/t3code-8afe4a1e** at 2026-10-08T22:29:09Z

Implemented a two-wave cream mark on a blue rounded square, matching the existing pond header and palette. The SVG is the source; Chromium canvas rasterization produced 16/32/48 px PNG entries in favicon.ico and the 180 px touch PNG. Both light and dark small-size previews are legible. A running isolated synthetic lake returned unauthenticated 200 responses with correct ICO/SVG/PNG MIME types, and its authenticated page declared all three formats. Existing web and recall tests passed. No manifest or frontend/image dependency was added because this request concerns browser identity, not an installable offline app.

**agent:codex/t3code-8afe4a1e** at 2026-10-08T22:30:35Z

PR #206: https://git.local.sothr.com/terva-sh/lampi/pulls/206. Clean model review at https://git.local.sothr.com/terva-sh/lampi/pulls/206#issuecomment-28255, run 72a1297d-ae2b-484f-a368-eace94699a40; reviewed head 3466363c36a9539934f025f64d315d2c28834b58 and base 0616211805ac616729b3a376ec62a1093a5f5da1. No findings. The review did not inspect binary images or execute routes; local small-size rendering and anonymous synthetic HTTP checks cover those limits. Dogfooding still runs the earlier dashboard build until a subsequent deployment.

## Summary

Implemented in PR #206: pond SVG, multi-size ICO and Apple touch icon, shared page links, and public root discovery routes. Synthetic HTTP checks confirmed all formats and MIME types, visual checks passed on light and dark backgrounds, and existing web/recall tests passed. Clean model review has no findings; merge awaits CI. Dogfooding icon rollout is separate.
