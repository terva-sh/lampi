---
schema: 3
id: TKT-01M61MZNMZ0ZC0CG223990GMCH
title: Session diagnosis engine with coded findings and suggestions
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
assignees: []
milestone: null
parent: TKT-01M61MYZ6H6J8MGNPWHS8MTJ1X
origin: null
dependencies:
  - TKT-01M61MZN03D4QZ6CAJPRNDFM6S
  - TKT-01M61MZMR6EHF8BAC5F4VNK9VN
  - TKT-01M61MZMXHF57GKYYRE9ZQ4S3S
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T16:20:10Z
updated_at: 2026-10-28T16:35:44Z
created_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
extensions: {}
---

## Description

A streaming pass over one session's events file that returns findings, each with a stable code, a severity, the event positions involved, the measured values, and a suggestion.

Initial codes, with thresholds from the calibration spike: `large_tool_result`, `large_tool_input`, `error_streak`, `retry_loop` (same tool and input hash), `repeated_read` (needs a per-harness parser of each harness's tool inputs for the path), `context_pressure` (input token growth, compactions, cache-read share), `orphan_call`, `orphan_result`, `unreadable_lines`.

Suggestions are static text keyed by code, in an embedded file versioned like internal/advisory/agents.json, so they are reviewable and change with a release. Hash inputs; never return content. Memory stays bounded regardless of session size.

## Acceptance criteria

- [ ] One streaming pass returns findings with code, severity, positions, values and suggestion.
- [ ] The initial codes are implemented with thresholds from the calibration spike.
- [ ] Suggestions live in an embedded, versioned file keyed by code.
- [ ] Memory stays bounded on the large synthetic transcripts from the performance workloads.

## Implementation plan

- Package internal/diagnose. `Diagnose(ctx, lines iter, Options) (Report, error)` reads one session's normalized events once, in order, holding only bounded state: per-tool counters, the last K input hashes per tool for retry loops, a bounded path→count map for repeated reads, open call ids (bounded), the running error streak, and usage deltas.
- Finding: `{code, severity, positions[] (capped), values{...}, suggestion{code, text, link}}`. Codes and default thresholds come from docs/session-diagnostics-calibration.md (TKT-01M61MZN0).
- Suggestions: internal/diagnose/suggestions.json embedded with go:embed and parsed at init like internal/advisory, with a test that every code has a suggestion and every suggestion a code.
- Input hashing uses sha256 of content_text for tool calls. File paths for repeated reads come from per-harness input fields recorded by the spike. Paths are compared hashed and never returned.
- The caller in internal/recall pins a generation with Reader.open and streams snapshot.lines, the same as selectSession. Expose `(*Reader).Diagnose(ctx, scope, uid)`, which returns ErrNotFound outside scope.
- Tests: one fixture per finding, plus a bounded-memory test on a large synthetic transcript (internal/synthetic) checking allocations stay flat.
