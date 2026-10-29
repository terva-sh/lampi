---
schema: 3
id: TKT-01M633Z98QMWZMBGGPGA89454P
title: "Release v0.10.0: session statistics, tool usage and diagnosis"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/ci
  - area/ops
  - area/docs
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-29T06:01:20Z
updated_at: 2026-10-29T06:21:13Z
created_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
extensions: {}
---

## Description

Release v0.10.0 from main at b6a0bbe: everything merged since v0.9.0 (PRs #214-#228). Minor version, as for v0.9.0: new features and an index schema change. The owner asked on 2026-10-29 to cut a new release, which authorizes the tag on both forges. Live lake and agent upgrades are not part of this ticket.

### Release notes


The lake now reports what happened inside the sessions, not only how many it holds.

**Usage page.** A new `/usage` page has three tabs, each with a range and harness filter:
- **Tokens:** token totals over time, split into input, output, cache read and cache write. The overview shows the same totals.
- **Errors:** error events and failed tool results over time.
- **Tools:** tool calls grouped into categories across harnesses, so Claude Code's `Bash`, terva's `bash` and Codex's `exec_command` all count as shell. The tab also shows:
  - each origin's share of calls beside its share of result bytes, where an origin is built in, an MCP server, a Claude Code plugin or a Codex extension;
  - result size and latency per tool;
  - the 20 largest tool inputs and results, linked to their transcript events;
  - a starting-context comparison between sessions that used an MCP server and sessions that did not, labelled as a correlation hint.

Operations also charts the normalize queue and the index lag over time.

**Tool usage statistics.** The statistics behind the Tools tab are also available from:
- `terva-lampi query tools`;
- `/api/web/v1/tools` and `/api/read/v1/tools`;
- the MCP tool `tool_stats`.

The statistics cover calls, sessions, error rates, input and result sizes, latency, calls over time and the largest blocks, grouped by tool or origin. Claude Code sessions also record which tools they were offered, so a server that was configured but never called shows up.

**Session diagnosis.** `terva-lampi query diagnose`, `/api/web|read/v1/sessions/{uid}/diagnosis`, `/api/web|read/v1/diagnosis` and the MCP tools `diagnose_session` and `diagnose_lake` report coded findings, each with a fixed suggestion. The transcript page has a Diagnosis panel that links each finding to its event. The findings are:
- oversized tool results and inputs;
- error streaks;
- retry loops;
- repeated reads of one file;
- context pressure;
- unanswered calls;
- noisy MCP servers;
- unknown tools.

Statistics and diagnoses return names, counts, sizes, times and event positions, never transcript content, and they stay within a read token's scope.

**Better data.** These changes take effect for a session when it is next normalized:
- Tool results name their tool for Claude Code, Codex, terva and Grok.
- OpenCode, Cursor and grokbot record whether a tool failed.
- Codex code-mode scripts project their inner tool calls.
- Cursor CLI sessions project their tool calls and results. In compacted sessions, older branches are ordered by the store's row order; where the order cannot be established, events carry `extra.order = "unestablished"`.

#### Upgrading from v0.9.0

- **Catalog:** no migration; the schema stays at 21.
- **Normalizers:** these change. A session that grows is normalized again when it is next uploaded. To fill in the rest, run `terva-lampi serve normalize --all` once after the upgrade. Positions move in Codex code-mode and Cursor CLI sessions.
- **Search index:** this goes from version 4 to 8. On the first start, `search.db` is deleted and rebuilt in the background, so search, usage and statistics cover fewer sessions until the rebuild finishes. Later upgrades from versions 5 to 7 happen in place.
- **Agents:** the Cursor CLI reader moves to version 2, which exports each blob's row number. After an agent upgrades, each of its Cursor CLI sessions uploads again once, in full. The other harnesses are unchanged, and older agents keep working with this lake.
- **Rehearsal:** an isolated upgrade from v0.9.0 to this release passed on synthetic data:
  - the catalog was unchanged, and an unchanged sync uploaded nothing;
  - the index rebuilt at version 8 with the offered tool names;
  - an appended session projected named tool results;
  - `serve normalize --all` queued the session;
  - `fsck` found 0 bad entries;
  - v0.9.0 then reopened the lake, and an unchanged sync again uploaded nothing.

**Rollback** is a binary or image swap. No catalog backup needs restoring. v0.9.0 rebuilds the search index again on its first start. Events already projected by this release stay readable, and a session normalized again by v0.9.0 returns to its old projection.

## Acceptance criteria

- [x] An isolated v0.9.0 upgrade rehearsal and the release gates pass on the synchronized main commit.
- [x] The tag is published on both forges, and the archives, checksums and images name the tag.
- [x] Both release bodies describe the features, the schema and normalizer changes, and rollback.

## Notes

**agent:claude-code/049834bc** at 2026-10-29T06:01:20Z

Gates on b6a0bbe: just ci passed on the merged tree before #228 merged (CI green on 888c7c6, identical tree to b6a0bbe). just release-check passed; just release-snapshot built all five archives and checksums. Isolated rehearsal (scratch release-v0100.* under the agent-scratch lampi root, rehearse.sh/rehearse.log): verified v0.9.0 linux_amd64 archive by checksum; v0.9.0 serve with web config and token, synthetic Claude session synced; candidate: migrate --check reports schema 21 up to date, index v4 rebuilt to v8 with offered tool rows, unchanged sync uploaded 0, append projected named tool_result, serve normalize --all queued 1, fsck 3 entries 0 bad; v0.9.0 reopened the lake (index rebuilt to 4) and unchanged sync uploaded 0; no errors in serve logs. No live config, token, lake or agent touched.

**agent:claude-code/049834bc** at 2026-10-29T06:21:13Z

Published. Annotated tag v0.10.0 on 6758420 (merge of #229), the same commit on both mains, pushed to both forges. GitHub release run 40827748993 succeeded: Build and Publish Release, including Verify the published binary reports the tag; Build and Publish Image with provenance attestation, including Verify each platform reports the tag. Forgejo release run 29448 succeeded. On each forge, all five archives pass sha256sum -c against that forge's checksums.txt, and linux_amd64 reports terva-lampi v0.10.0 (675842093001). The two forges build separately, so their checksums differ, as they did for v0.9.0. GHCR ghcr.io/terva-sh/lampi:0.10.0 holds linux/amd64 and linux/arm64, tags 0.10 and latest exist, and the image reports v0.10.0 (6758420). Both releases are titled 'v0.10.0 — Session statistics, tool usage and session diagnosis'. Each body is the notes from this ticket, followed by goreleaser's install section and changelog.

## Summary

v0.10.0 is published on both forges from 6758420, with archives, checksums, and a multi-platform GHCR image, all verified against the tag. Both release bodies carry the upgrade notes: no catalog migration; normalizers changed, so run serve normalize --all; the search index is rebuilt from v4 to v8; Cursor CLI sessions re-upload once; rollback is a binary swap.
