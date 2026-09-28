---
schema: 3
id: TKT-01M3MC0S673XAKM5FV2X96DWTV
title: "Deploy: single-replica Kubernetes manifests for k3s home servers"
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area/ops
assignees: []
milestone: null
parent: TKT-01M3MC023P4A5H7PTF662QSSM8
origin: null
dependencies:
  - TKT-01M3MC0QVMMW0TQ6RGAYDZEF82
  - TKT-01M3MC0R3B98FYBSM1Q42TA543
  - TKT-01M3MC0RA026HX6ZKSKG89F5NA
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T16:01:57Z
updated_at: 2026-09-28T16:01:58Z
created_by:
  id: agent:claude-code/aa1afd80
  name: ""
updated_by:
  id: agent:claude-code/aa1afd80
  name: ""
extensions: {}
---

## Description

Some home servers run k3s or another small Kubernetes. Provide a plain manifest set (and a Helm chart only if the manifests turn out to need templating) for one lake:

- **Workload.** A `StatefulSet` or `Deployment` with `replicas: 1` and strategy `Recreate`, because `serve` is single-writer and a rolling update would start a second writer while the first still holds `lake.lock`.
- **Storage.** A `PersistentVolumeClaim` on local storage (`ReadWriteOnce`, the local-path provisioner on k3s). A warning against NFS-backed storage classes.
- **Migrations.** An init container running the migration subcommand from the migrations ticket, so a failed migration shows as `Init:Error` instead of a crash loop.
- **Probes.** `httpGet /healthz` probes. A `startupProbe` long enough for a migration.
- **Shutdown.** `terminationGracePeriodSeconds: 60`.
- **Secrets.** A `Secret` for the tokens and the OIDC client secret, mounted as files. A `ConfigMap` for `profiles.json` and the web config.
- **Networking.** A `Service`, and an `Ingress` example with TLS and a body size annotation for the common controllers (Traefik on k3s, ingress-nginx).
- **Hardening.** A `securityContext` for non-root 65532: `readOnlyRootFilesystem`, drop ALL, `seccompProfile: RuntimeDefault`, and `fsGroup` so the volume is writable.
- **Backups.** Point to the backups ticket for a `CronJob`, including the `ReadWriteOnce` limitation.

Low priority: the compose path covers most home servers. Don't start this before the image, health, and migration tickets are done.

## Acceptance criteria

- [ ] Manifests run one lake with Recreate strategy, a local PVC, probes and a hardened securityContext
- [ ] Migrations run in an init container
- [ ] The manifests were applied to a real k3s cluster
