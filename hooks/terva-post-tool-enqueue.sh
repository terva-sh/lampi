#!/bin/sh
# Supported optional acceleration for a terva post_tool_use hook.
# make build does not install this file. Wiring is in deploy/README.md.
#
# The filesystem watch is the source of truth. This script only asks a
# running terva-lampi agent to sync now. It reads agent.pid in the XDG
# state directory and sends SIGUSR1 when that process is the terva-lampi
# executable. It does not upload, and it does not wait for the sync to
# finish. If the agent is absent, the pid is not terva-lampi, or the
# signal cannot be delivered, the script exits 0. The watch, or the
# next agent start, still uploads the bytes.
#
# Unix only. terva passes the tool event on stdin; this script does
# not read it. A command line that merely mentions terva-lampi is not
# the agent and is not signalled.

set -eu

state=${XDG_STATE_HOME:-${HOME:-}/.local/state}
pidfile="$state/terva-lampi/agent.pid"

if [ ! -f "$pidfile" ]; then
	echo "terva-lampi: post_tool_use: agent is not running; the filesystem watch will upload" >&2
	exit 0
fi

pid=$(tr -d '[:space:]' <"$pidfile" || true)
case "$pid" in
'' | *[!0-9]*)
	echo "terva-lampi: post_tool_use: agent.pid is not a pid; left it" >&2
	exit 0
	;;
esac

if ! kill -0 "$pid" 2>/dev/null; then
	echo "terva-lampi: post_tool_use: agent pid $pid is not running; the filesystem watch will upload" >&2
	exit 0
fi

# Linux: /proc/<pid>/exe is the binary, so a lampi symlink to
# terva-lampi still counts, and an argv that only contains the name
# does not. Other Unix has no /proc; the invoked basename is the check
# the launchd unit satisfies by starting terva-lampi.
base=""
exe=""
if [ -L "/proc/$pid/exe" ]; then
	exe=$(readlink "/proc/$pid/exe" 2>/dev/null || true)
fi
if [ -n "$exe" ]; then
	exe=${exe% (deleted)}
	base=${exe##*/}
else
	cmd=$(ps -p "$pid" -o command= 2>/dev/null || true)
	while :; do
		case $cmd in
		[[:space:]]*) cmd=${cmd#?} ;;
		*) break ;;
		esac
	done
	first=${cmd%%[[:space:]]*}
	base=${first##*/}
fi

if [ "$base" != "terva-lampi" ]; then
	echo "terva-lampi: post_tool_use: pid $pid is not terva-lampi; not signalling" >&2
	exit 0
fi

if kill -USR1 "$pid" 2>/dev/null; then
	echo "terva-lampi: post_tool_use: asked the agent to sync" >&2
else
	echo "terva-lampi: post_tool_use: could not signal the agent; the filesystem watch will upload" >&2
fi
exit 0
