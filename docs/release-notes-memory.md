# Unreleased: lower memory use for growing transcripts

The capture agent prepares one session at a time and streams verified artifacts
from private file snapshots. The lake streams Claude, Codex and terva JSONL into
compressed events and bounded Parquet output instead of keeping a complete
transcript and event slice in memory. Pending normalize generations coalesce,
and two generations of one session cannot project concurrently.

Three-process synthetic comparisons reduced median agent-only peak RSS by
83–94% versus the already-batched control. The subsequent lake streaming
comparison reduced combined agent/lake median peak RSS from 418 to 235 MiB
for three 32 MiB sessions and from 665 to 174 MiB for one 128 MiB session.
These are isolated workloads, not measurements of an upgraded production
service. CPU is roughly unchanged in that comparison; allocation and GC work
increase. Full files are still copied/hashed and changed sessions are still
fully projected. See [performance.md](performance.md) for controls, ranges,
profiles, disk/cache costs and deployment choices.

New optional controls:

- `serve --normalize-workers N` (default 2) and
  `--normalize-input-budget-mib N` (default 256) control concurrency by logical
  input size. A job larger than the budget runs alone. This is not an RSS cap.
- Both `agent` and `serve` accept `--diagnostics-dir NEW_DIR` and
  `--diagnostics-duration 30s` for private timed CPU/heap/runtime captures.
  Diagnostics are off by default and add no network listener.
- The existing optional metrics listener reports runtime memory/GC/process CPU,
  admission/coalescing and queue wait observations.

Schema and upgrade behavior: no catalog migration is added since v0.8.0,
normalized event schema remains version 1, and normalizer semantics are
unchanged. This work does not queue a full re-normalization on upgrade. Existing
derived event/Parquet files remain readable; a subsequent changed session uses
the new streaming worker. Capture permissions and debounce defaults are unchanged.
Private temporary files add disk I/O; provision space for current-session agent
snapshots plus active lake spools/output. Lake startup removes abandoned private
projection stages while holding lake.lock before starting workers.

Other changes already on main since v0.8.0 include dashboard pagination and
Operations maintenance controls; include those in the final release description
when selecting the release commit. The local release checklist and validation
record are in [performance.md](performance.md).

Rollback: remove newly added CLI flags when returning to an older binary, stop
normally so durable jobs can drain, and retain the existing catalog/CAS and agent
state. These changes add no incompatible schema or output format. Restore the
previous worker, memory-limit and debounce settings together with the binary;
verify normalize readiness, queue age, upload counters and quarantine behavior.
If a later change on the chosen release commit adds a migration, follow that
migration's backup/rollback procedure instead.

This document prepares release notes. It does not select a tag, publish archives
or images, install a binary, restart a live service, or change capture policy.
