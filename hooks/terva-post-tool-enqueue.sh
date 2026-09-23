#!/bin/sh
# Example terva post_tool_use hook. Not installed by default.
#
# Acceleration only. terva-lampi agent watches $TERVA_HOME/sessions
# whether or not this script runs. A running agent writes agent.pid in
# its state directory and, on Unix, treats SIGUSR1 as "sync now". This
# script sends that signal when the pid's command line is terva-lampi.
# If the agent is not running, the script exits 0. The next watch, or
# the next time the agent starts, still uploads the bytes.
#
# Point a terva post_tool_use hook at this file when you want the nudge.
# The filesystem watch remains the source of truth.

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

cmd=$(ps -p "$pid" -o command= 2>/dev/null || true)
case "$cmd" in
*terva-lampi*) ;;
*)
	echo "terva-lampi: post_tool_use: pid $pid is not terva-lampi; not signalling" >&2
	exit 0
	;;
esac

if kill -USR1 "$pid" 2>/dev/null; then
	echo "terva-lampi: post_tool_use: asked the agent to sync" >&2
else
	echo "terva-lampi: post_tool_use: could not signal the agent; the filesystem watch will upload" >&2
fi
exit 0
