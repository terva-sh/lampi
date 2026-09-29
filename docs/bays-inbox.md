# Keeping the inbox empty

A lake's default bay is its inbox ([Bays](policy.md#bays)). A session
lands there when nothing places it: the agent asked for no bay, or for
none it may write, and no lake rule added one. Only admins and
principals granted the default bay by name read it. A session left in
the inbox is one nobody has decided about, so the aim is an inbox with
nothing in it.

Every command here runs on the lake host while `serve` runs. Each
change is written to `audit.jsonl`.

## After the upgrade

The upgrade puts every stored session in the default bay and grants
every device write on it. Viewers and operators keep reading it, so
nothing changes until an admin makes a second bay. To sort an existing
lake:

1. Make the bays: `serve bays create client-x`.
2. Look at what is there: `serve bays inbox` lists each session with
   its cwd, remote and why it is in the inbox.
3. Move sessions by what they have in common, with `--dry-run` first:

   ```sh
   terva-lampi serve bays move client-x --git-remote-prefix git@git.example:client-x --dry-run
   terva-lampi serve bays move client-x --git-remote-prefix git@git.example:client-x
   ```

   A move takes sessions out of `--from`, the default bay unless named,
   and puts them in the target. The filters are `--project`,
   `--git-remote`, `--git-remote-prefix`, `--cwd-prefix`, `--device`
   and `--harness`, and every one given must match. A move needs at
   least one. A move from the default bay also takes a matching session
   that is in no bay.
4. Grant the bays to the groups that read them:
   `serve bays grant client-x --group client-x-team --read`.
5. Once the inbox holds only what should stay private to admins, take
   the default grants away from viewer and operator groups:
   `serve bays revoke default --group readers --read`.

## Keeping new sessions out

Two things place a new session, and either one keeps it out of the
inbox:

- **The agent asks.** A lake entry in the machine's `config.json` names
  bays for the sessions its rules match
  ([Asking for bays](registration-and-lakes.md#asking-for-bays)), and
  the device is granted write on them:
  `serve bays grant client-x --device laptop --write`. `terva-lampi bays`
  on the machine shows what it may write, and `terva-lampi bays which`
  what a folder asks for.
- **The lake adds.** `serve bays rule client-x --add --git-remote-prefix
  git@git.example:client-x` puts every matching session in the bay,
  whatever the agent asked. `--deny` keeps a session out of a bay, and
  `--hold` sends a new session to a review bay alone.

A rule applies to each manifest from the time it is added. To apply it
to what is already stored, run `serve bays apply-rules --dry-run`, then
without `--dry-run`. It only adds, so it does not take a session out of
the inbox; follow it with a `move` out of the default.

Rules read a `cwd_prefix` as the folder and everything under it. A rule
for one folder only is a `cwd_hash`.

## Why the reasons matter

`serve bays inbox` gives each session one or more reasons:

| Reason | What to do |
|--------|------------|
| no bay asked for and no rule added one | Add a lake rule, or a request rule on the machine, for sessions like it. Move this one. |
| asked for the default bay, as X | The machine asks for the default bay by name. Change its request rules, or move it. |
| added to the default bay by rule N | A lake add rule names the default bay. Point the rule at the bay these sessions belong in. |
| placed in another bay and still in the default | A rule or a move added it elsewhere; both only add. `serve bays move BAY --from default` with a filter takes it out of the default. |
| asked for bay X: refused, not granted | The device asked for a bay it may not write. Grant it, or fix the machine's config. |
| asked for bay X: refused, no such bay | The machine asks for a bay the lake does not have, or by a name since changed. `serve bays alias` keeps an old name working. |
| held by hold rule N into bay X | A hold rule sent it for review. `serve bays release UID` places it as it asked. |
| flagged by hold rule N into bay X | A stored session a hold rule began to match. It keeps its bays until released. |
| in no bay | No write should leave this. `serve fsck` reports it too. `serve bays move BAY` with a filter places it, as a move from the default bay. |

A reason that keeps coming back is a rule that is missing. Fixing the
rule empties the inbox for the sessions not yet made, which a move
cannot do.

## Turning the default off

`serve bays default off` refuses, at ingest, a new session that nothing
places. The agent keeps it on the machine, lists it under `no_bay` in
`status`, and sends it again every pass, so it uploads once a grant or a
rule places it. Turn the default off once rules and requests place every
new session, and the inbox holds only what was there before.
