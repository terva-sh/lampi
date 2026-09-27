# Why the command is terva-lampi

The command is **`terva-lampi`**. The repository is `lampi`. They are
different on purpose. Back to the [documentation index](README.md).

lampi is Finnish for a pond, a small lake. The bytes from the machines
you work on collect there.

## A bare lampi is already taken

A bare `lampi` on `PATH` already means something else.
[neurobin/lampi](https://github.com/neurobin/lampi) is a LAMP installer
that copies itself to `/usr/local/bin/lampi` (`lampi -i`, `lampi -n`).
It is old and still documented in blog posts. Shipping that name as the
primary command would replace it, or be replaced by it.

`terva-lampi` is free of that collision and sits in the same family as
the terva harness.

## Adding a lampi shortcut

An install may still add a `lampi` symlink for people who want the
short name. Before it does, check what `lampi` already is. If
`command -v lampi` is a shell script, or the file mentions neurobin,
leave it alone. Invoking this program under the name `lampi` prints
that warning itself.
[deploy/install-lampi-alias.sh](../deploy/install-lampi-alias.sh) makes
the symlink and refuses to replace an existing `lampi`.

## Other products named Lampi

The word also belongs to other products: Lampi AI at lampi.ai, a
children's app, and Finnish companies. The metaphor stays. The command
people type does not depend on winning that search.
