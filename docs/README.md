# Documentation

Start with the [project README](../README.md) for what lampi is and a
first run. The pages below are grouped by what you are trying to do.

## Use lampi on your machines

| Page | Read it when |
|------|--------------|
| [Getting started](getting-started.md) | You want a lake on one machine with one project in it |
| [Harnesses](harnesses.md) | You want to know which transcripts are read, and from where |
| [Allowlist and redaction](allowlist-and-redaction.md) | You are about to allow a project, or a file was quarantined |
| [The agent](agent.md) | You want to know when the agent pushes, how it retries, or which signals it answers |
| [Registration and lakes](registration-and-lakes.md) | You are adding a machine, or sending sessions to more than one lake |

## Run a lake

| Page | Read it when |
|------|--------------|
| [VPS bring-up](vps-bringup.md) | You are setting up a hosted lake: disk, loopback serve, TLS, devices, backup |
| [Run a lake in a container](container.md) | You run the lake with Docker Compose behind Caddy, on a home server or a VPS |
| [Serving the dashboard](web-dashboard.md) | You are turning on the OIDC browser dashboard |
| [Packaging examples](../deploy/README.md) | You want the systemd and launchd units, the harness map, or the hook |
| [Phase 0 policy](policy.md) | You need the placement, retention, key, and encryption decisions |

## Reference

| Page | What it covers |
|------|----------------|
| [Command reference](cli.md) | Every command, export formats, and setting precedence |
| [Capture protocol 1](protocol.md) | The wire protocol between the agent and the lake |
| [Browser API](web-api.md) | The dashboard's metadata, transcript, search, and excerpt routes |
| [Architecture](architecture.md) | What each package does, the data flow, and what is not built |

## Work on lampi

| Page | What it covers |
|------|----------------|
| [Developing lampi](development.md) | Build, tests, dev recipes on a machine that runs lampi, and git-ticket |
| [Pull requests and reviews](pr-reviews.md) | Forgejo and GitHub, `terva-review`, and keeping `main` equal |
| [Dashboard and retrieval plan](web-ui-plan.md) | The design record for the web releases |
| [e2e/README.md](../e2e/README.md) | The synthetic container, the browser smoke, and the go-live drills |
| [Why the command is terva-lampi](naming.md) | The name collision with a LAMP installer, and the optional `lampi` shortcut |
