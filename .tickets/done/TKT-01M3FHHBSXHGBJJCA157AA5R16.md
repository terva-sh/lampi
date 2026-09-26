---
schema: 3
id: TKT-01M3FHHBSXHGBJJCA157AA5R16
title: "Onboarding: end-to-end validation of both paths and operator docs"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
  - area/docs
assignees: []
milestone: null
parent: TKT-01M3FHHBCJ12FXKNTB6Z138F6N
origin: null
dependencies:
  - TKT-01M3FHHBREE42QFPAN0YFHSH98
  - TKT-01M3FKS3XHYM1QXR4Q56SGWY6Y
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-26T19:02:12Z
updated_at: 2026-09-26T22:17:37Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Part of the agent onboarding epic. Prove both onboarding paths end to end, then rewrite the operator docs around them.

### Validation

Use real `serve` subprocesses, as `goLiveServe` in `internal/cli/golive_test.go` does, and isolated XDG directories. No production host and no real credentials.

- Path 1: mint a code on lake A, register a fresh client from it, and sync. The device appears by name, the base configuration applies, and a second sync posts nothing.
- Path 2: start an agent with no lake and confirm that it uploads nothing. Register lake A and then lake B without restarting it. Sessions allowed for A only reach A, sessions allowed for both reach both, and a local deny stops both.
- Failure cases: a used code, an expired code, a tampered code, the wrong key at the code's URL, a revoked device, and lake B down while A keeps syncing.
- Upgrade: a legacy single-lake client state and a legacy token file on the lake both upgrade in place, and the next sync uploads nothing.

### Docs

Rewrite `docs/vps-bringup.md` "Device token" and "Check, then point the agents", the README quickstart, and `deploy/README.md` around `serve register` and `terva-lampi register`. Keep the manual token-file path documented as the fallback. Cover `serve identity set-url`, an example profile file under `deploy/`, and rate limits at the proxy for `/v1/register` and the key endpoint in the Caddy example. State that Windows needs an agent restart to add a lake.

## Acceptance criteria

- [x] Real-subprocess tests cover path 1, path 2 with two lakes, the failure cases and the legacy upgrade
- [x] The vps-bringup, README and deploy docs lead with registration and keep the manual token path as a fallback

## Implementation plan

golive_onboard_test.go (build tag golive): each lake is a serve subprocess via goLiveServeArgs (goLiveServe now passes extra flags through LAMPI_GOLIVE_PROCESS_ARGS) with a token directory and a profiles.json; clients use closed XDG dirs and explicit harness roots. Drills: fresh machine (mint, register, sync, device by name, profile applied, second sync posts nothing); standalone agent with two lakes added live (only-a / both / locally denied); failures (tampered, wrong key, expired by the lake, used code twice, lake b down while a syncs, revoked device); legacy upgrade (single-lake client layout moved in place, token-file device bound, nothing re-sent). Docs: vps-bringup Devices (registration first, token file as fallback, set-url, profiles, set-profile), Check-then-point (register, --install-service, Windows restart), proxy rate limits for the open routes (caddy-ratelimit and nginx limit_req); README quickstart leads with registration; deploy/README and deploy/profiles.json.example (loaded by a test); policy.md says registration has landed.

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-26T22:17:37Z

What the drills do and do not cover:

- Every lake is a separate `serve` process; the drill checks the pid. The agent in the path-2 drill runs in-process. SIGHUP reaches it because `register` signals the `agent.pid` holder, which is the test process. Only the lake side is a subprocess.
- The "legacy token file on the lake" upgrade is exercised as a token-file device from this release: it is named from its comment, bound on first manifest, and survives a serve restart. It does not start from a real schema-4 catalog written by the previous release. The catalog step from 4 to 6 is covered by the catalog schema tests, which run every migration in order on each open.
- The client-side legacy state is the real single-lake layout. The test builds it with `legacyLayout`, which moves `lakes/default` back to the top.
- The expired-code case uses a code with a 1s expiry, redeemed 1.5s later. That is inside the client's five-minute skew allowance, so this shows the lake refusing it as the authority. The client-side expiry refusal is covered by `TestRegisterRefusesEachCheck`.
- Proxy rate limits: core Caddy has no limiter, so the Caddy example needs the caddy-ratelimit module. The nginx example uses core `limit_req`. Neither is exercised by a test.

Evidence:

- `go test -tags golive ./internal/cli -run TestGoLive` is green, including the four `TestGoLiveOnboard*` drills.
- `TestDeployProfilesExampleLoads`
- The full `-race` suite and vet on Linux, golive and Windows are green.

## Summary

Four golive drills against serve subprocesses prove path 1, path 2 with two lakes added to a running standalone agent, the failure cases and the legacy upgrade. vps-bringup, the README quickstart, deploy/README and policy.md now lead with serve register / terva-lampi register, keep the token file as the fallback, and cover set-url, profiles (with deploy/profiles.json.example), proxy rate limits for the open routes, and the Windows restart.
