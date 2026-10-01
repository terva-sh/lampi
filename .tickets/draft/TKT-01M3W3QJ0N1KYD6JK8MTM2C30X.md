---
schema: 3
id: TKT-01M3W3QJ0N1KYD6JK8MTM2C30X
title: "Spike: how a laptop lake's dashboard signs in without an IdP"
type: spike
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/auth
  - policy
  - question
assignees: []
milestone: null
parent: TKT-01M3W3PZSDCB5FGKJS06MSZH65
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-01T16:11:02Z
updated_at: 2026-10-01T16:20:47Z
created_by:
  id: agent:claude-code/0f3154cf
  name: ""
updated_by:
  id: agent:claude-code/0f3154cf
  name: ""
extensions: {}
---

## Description

The dashboard needs OIDC, and a laptop lake has no IdP. Decide how a
laptop lake's owner signs in. The question was raised with Dex in
mind; record each option and why it wins or loses.

### Options

1. **Dex on loopback with TLS.** Dex serves `https://127.0.0.1:5556`
   with a certificate from a local CA (mkcert or one lampi makes) that
   is added to the system trust store. No lampi change, but a CA in
   the trust store is a real cost on a laptop.
2. **Dex on loopback over HTTP, with a lampi change.** Allow an
   `http://` issuer only when the issuer host and `base_url` are both
   loopback. Today `webconfig.HTTPSURL` refuses it and
   docs/web-dashboard.md promises no bypass, so this is a policy
   change for docs/policy.md as well as code.
3. **A loopback-only owner mode with no OIDC.** `serve` binds the
   dashboard to loopback and treats the local user as admin, the way
   `serve` without a token accepts loopback requests. No IdP at all,
   but every local process and every browser tab on the machine gets
   admin, including raw artifact reads.
4. **An issuer built into serve**, on the lines of internal/testidp.
   Lampi would then own an IdP's security.

### Facts to check against Dex before choosing it

- Lampi grants nothing without a group claim mapped to a role. Check
  whether Dex's `staticPasswords` emit a `groups` claim, or whether a
  connector (LDAP, a mock connector, GitHub) is needed for groups.
- Operator actions need `auth_time` after a `max_age` re-login. Check
  whether Dex honors `max_age` and returns `auth_time` in the ID
  token. Without it, registration codes cannot be minted from the
  dashboard.
- Check Dex's release cadence and its packaging for macOS
  (Homebrew) and Linux, since a laptop install depends on it.

The outcome goes in the ticket with the losing options and their
reasons, and the Dex ticket depends on it.

## Acceptance criteria

- [ ] Each option is recorded with why it wins or loses
- [ ] Dex's groups claim and max_age/auth_time support are checked and recorded
- [ ] Any change to the HTTPS issuer rule is reflected in docs/policy.md

## Notes

**agent:claude-code/0f3154cf** at 2026-10-01T16:20:47Z

Owner, 2026-10-01: a Mac laptop already runs Dex for this, with a
self-signed certificate that the machine trusts through Apple's
tooling (the system keychain). That is option 1 in practice on macOS,
and the owner thinks it may be a good fit there. The spike should
start from that setup: record how the certificate was made and
trusted, and check it against the groups and max_age/auth_time
questions above. Linux still needs its own answer for the trust store
step.
