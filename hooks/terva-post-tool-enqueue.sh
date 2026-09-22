#!/bin/sh
# Example terva post_tool_use hook. Not installed by default.
#
# A hook can nudge a running agent that a session grew. It is not the
# source of truth: terva-lampi discovers JSONL under $TERVA_HOME/sessions
# whether or not this script ever runs. The enqueue itself is not built.
echo "terva-lampi: post_tool_use received; enqueue is not implemented" >&2
exit 0
