---
schema: 3
id: TKT-01M3558DJ8E4D9FK6VV8PHRD1G
title: Device-token auth hygiene (--token-file, hashed server-side)
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area/auth
  - phase/1-mvp
assignees: []
milestone: mvp
parent: TKT-01M3558DF4KG977332Q7NG30VB
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:11Z
updated_at: 2026-09-22T22:53:47Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/19a7
  name: Cursor cloud agent
extensions: {}
---

## Description

Per-device 256-bit bearer tokens, hashed at rest on server, client reads --token-file only (never argv). Single-tenant, many devices.

## Acceptance criteria

- [x] The server stores a hash of the device token
- [x] The client reads the token only from --token-file
- [x] Each device has its own token

## Implementation plan

Serve loads --token-file through auth.LoadDevices. Each line is one device's 256-bit token, or a directory holds one file per device. Plaintext lines are hashed with SHA-256 and the file is rewritten to sha256:<hex> lines, mode 0600. The process keeps the hashes and compares the bearer by hashing it. The client still sends the raw token, and it still reads that token only from --token-file. --token on any command is refused.

login keeps generating a fresh token per device and does not print it. The lake copy is the one that is hashed; the client file stays the secret.

## Summary

terva-lampi serve --token-file loads one token per line, or one file per device when the path is a directory. Each plaintext token is replaced with a sha256 line at mode 0600. The process keeps the hashes and checks the bearer by hashing it. login still writes a fresh 256-bit token per device and does not print it. The client reads that file only through --token-file. --token is refused on every command.
