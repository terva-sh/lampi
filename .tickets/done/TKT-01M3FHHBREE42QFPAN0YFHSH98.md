---
schema: 3
id: TKT-01M3FHHBREE42QFPAN0YFHSH98
title: "CLI: terva-lampi register and lakes list/remove for both paths"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/agent
  - area/auth
assignees: []
milestone: null
parent: TKT-01M3FHHBCJ12FXKNTB6Z138F6N
origin: null
dependencies:
  - TKT-01M3FHHBJAZ54CXQ7J1Y88XGVF
  - TKT-01M3FHHBKTMJCHCMQQQJZKQTMF
  - TKT-01M3FP115FH6DV3V5WGN3XHPJK
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-26T19:02:11Z
updated_at: 2026-09-27T01:40:49Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Part of the agent onboarding epic. Add the client commands for both onboarding paths.

- `terva-lampi register` reads a code from stdin, from an interactive prompt, or from `--code-file`, never from argv. Before it redeems the code it checks, in order:
  1. The code's signature verifies against the key it carries, and the code has not expired. The client allows five minutes of clock skew and, past that, names the local clock as a likely cause. The lake remains the authority on expiry.
  2. The URL is `https://`, or `http://` on loopback, which is the rule the client already applies.
  3. `GET /.well-known/terva-lampi/keys` at that URL, with a fresh random nonce, lists the code's key as active under the code's lake id, and its signature over the nonce verifies against that key.
  4. The operator confirms the URL, lake id and key fingerprint. Interactive use prompts. `--fingerprint SHA` checks non-interactively against the value `serve identity` prints. A run with no terminal and no `--fingerprint` refuses.

  Any failure refuses the registration and names the check that failed. Then it generates the device token, redeems the code, writes the token file at 0600 and the lake entry into `config.json`, stores the verified base configuration, and signals a running agent to reload. On Windows it says the agent must be restarted instead. It never prints the token.
- **Path 1, a fresh machine:** `terva-lampi register` with no existing config creates the config directory, the state directory and the lake entry. `--install-service` optionally writes and enables the systemd user unit or the launchd agent from `deploy/`, and suggests `loginctl enable-linger` on Linux when the user has no lingering session.
- **Path 2, a standalone agent:** the agent is already running with no lake, or with other lakes. `register` adds one lake and the running agent picks it up.
- `terva-lampi lakes list` and `terva-lampi lakes remove NAME` manage the set. `remove` keeps that lake's state directory unless `--purge-state` is given.
- A new client that meets a lake with no key endpoint refuses with a message that the lake must be upgraded first.
- A second `register` for a lake already present, matched by lake id, refuses and names the existing entry. Re-registering takes an explicit `--replace`.

## Acceptance criteria

- [x] register reads the code from stdin, a prompt or a file, never from argv, and never prints the token
- [x] register refuses a tampered or expired code, a non-loopback http URL, or a lake whose hello key differs from the code
- [x] On a fresh machine register creates everything needed to sync, and --install-service enables the user unit
- [x] Against a running agent, register adds the lake live, and a duplicate lake id needs --replace
- [x] lakes list and lakes remove work, and remove keeps state unless --purge-state is given
- [x] register refuses when the published key list at the code's URL does not list the code's key as active or its nonce signature fails
- [x] register shows the URL, lake id and fingerprint and needs confirmation, or --fingerprint when there is no terminal

## Implementation plan

register: read the code from --code-file, stdin, or a terminal prompt (never argv). Check 1 regcode.Decode + expiry with 5 min skew; check 2 upload.CheckToken rule on the URL; check 3 verifyKeyList (shared with serve register's URL check): FetchKeys over a fresh nonce, signature by the code's key, nonce and lake id match, key listed active; a 404 is 'upgrade the lake'; check 4 fingerprint via --fingerprint or a y/N prompt on a terminal, refused otherwise. Then: duplicate lake id refuses unless --replace; name = --lake, the existing entry's, default when free, else the URL host's first label; token generated, written to tokens/<name>.token.pending, redeemed with its hash and the lake's machine id, renamed into place; response profile verified against the new pin and cached; config.SetLake writes the pinned entry (raw-JSON edit keeps unknown keys); reloadAgent signals a running agent or says restart on Windows; --install-service writes a systemd user unit (ExecStart = this binary, ExecReload = HUP) or launchd plist, enables it (--now unless an agent already runs), and suggests enable-linger. lakes list prints lake and profile lines; lakes remove edits config.json, removes the register-written token, signals the agent, keeps state unless --purge-state (refused while an agent holds agent.pid).

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-26T21:24:33Z

From TKT-01M3FP115: after writing config.json, register and lakes remove must print reloadAgent(stateDir) (internal/cli/pidfile_*.go). On Unix it SIGHUPs a running agent; on Windows it says a restart is required, which is TKT-01M3FP115's third criterion. When writing config.json keep an empty lakes map as {} (no omitempty on that write): an empty map means standalone, a missing one means the loopback lake.

**agent:claude-code/e4a47e8c** at 2026-09-26T21:57:53Z

Decisions:

- **Token path.** Every registered lake gets an explicit `token_file` at `tokens/<name>.token`, the default lake included.
  - Rejected: reusing the legacy `token` path for the default lake. A machine that once ran `login` for a loopback lake has that file, and registering would have refused, or overwritten it with `--replace`.
- **Token write order.** The token goes to `<path>.pending` first and is renamed into place only after the lake accepts the code. A refused code leaves the old token, or none, where it was.
- **Re-registering a machine.** Redeem now lets a machine id that belongs only to a revoked device be bound again. It clears the id from the revoked row in the same transaction, because `devices_machine` is a unique index.
  - Without this, `register --replace` from the same machine could never succeed: the old device keeps the machine id, and `unbind` is the only way to clear it.
  - The operator flow is now: revoke the old device, then run `register --replace`. The 409 message says so.
- **Service unit.** `--install-service` generates the unit with `ExecStart` set to `os.Executable()`, rather than copying `deploy/`'s `~/.local/bin` path.
  - It adds `ExecReload=/bin/kill -HUP $MAINPID`, so `systemctl reload` reloads lakes.
  - It never overwrites an existing unit file.
  - When an agent is already running it enables without `--now`, because a second agent on the same state directory would exit on the pid lock.
- **Prompt echo.** The interactive prompt echoes the code. Hiding it needs `golang.org/x/term`, a new dependency. The code is single-use and short-lived, and it goes to scrollback, not to logs. Recorded here in case the owner wants the dependency.
- **Env over config for the default lake.** `LAMPI_SERVER` in the environment still overrides the registered default lake's server, by the existing precedence rule. It is not changed here.

Evidence:

- `TestRegisterOnAFreshMachineThenSync`: config, token 0600 and not printed, cached profile, then sync uploads. It also covers a duplicate that needs `--replace`, replace before revoke giving 409, and replace after revoke.
- `TestRegisterRefusesEachCheck`: tampered, expired, plain http, another key, wrong fingerprint, no terminal, empty stdin, code as argument. Nothing is written, and the code still works afterwards from `--code-file` with an unprefixed fingerprint.
- `TestRegisterRefusesALakeWithNoKeyEndpoint`
- `TestRegisterAsksOnATerminal`
- `TestLakesListAndRemove`: token removed, state kept, purge refused beside an agent and done without one.
- `TestRegisterInstallsTheUserService`: runs with systemctl and loginctl faked, so no test touches real services. A real `systemctl --user enable` has not been exercised on a machine.
- `TestRegisterAddsALakeToARunningStandaloneAgent`: SIGHUP, reload, upload.
- `TestRegistrationRedeemsOnce` extended with the case above.
- Full `-race` suite, the golive drills, and vet on windows and darwin are green.

**agent:claude-code/e4a47e8c** at 2026-09-27T00:04:28Z

Review 933 on PR #18, all four accepted in d874bdd. lakes remove --purge-state takes agent.pid before changing anything, and refuses with nothing removed while an agent runs. On that path it does not signal the agent, since it would find its own pid. --replace takes over only an entry pinned to the code's lake id, or an unpinned one on the same server (pre-pin entries), keeps the existing name, and refuses a different --lake. chooseLakeName shortens the base so -N stays within 32 characters.

**agent:claude-code/e4a47e8c** at 2026-09-27T01:01:02Z

Review 976 on PR #18, both accepted in 99c8a62. lakes remove refuses any name that fails ValidLakeName before it builds a path; before this, '..' with --purge-state removed the whole state directory. A config.json holding null is edited as an empty config, the way LoadFile already reads it.

**agent:claude-code/e4a47e8c** at 2026-09-27T01:40:49Z

Review 987 on PR #18, both accepted in d52087c. register --replace always writes the new token to this lake's own tokens/<name>.token and leaves the replaced entry's token file alone, since that file may be hand-placed or shared. lakes remove reports a token it could not delete, after finishing the rest of the removal. Not fixed: another lake's entry naming this lake's tokens/<name>.token would still be overwritten. Filed with the config-lock gap as TKT-01M3G (see the draft about config.json locking).

## Summary

terva-lampi register joins a lake from a code on stdin, a prompt or a file after four named checks (code signature and expiry, https URL, key list over a nonce, fingerprint confirmation), writes the token, pinned lake entry and profile, and signals a running agent; --install-service enables the user unit. terva-lampi lakes lists and removes lakes, keeping state unless --purge-state.
