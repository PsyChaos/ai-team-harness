#!/usr/bin/env bash
set -euo pipefail

INPUT="$(cat)"
EVENT="$(jq -r '.hook_event_name // empty' <<<"$INPUT")"
COMMAND="$(jq -r '.tool_input.command // .tool_input.cmd // .tool_input.shell_command // empty' <<<"$INPUT")"

[[ -z "$COMMAND" ]] && exit 0

reason=""
if grep -Eqi '(^|[;&|[:space:]])sudo([[:space:]]|$)' <<<"$COMMAND"; then
  reason="sudo is blocked for autonomous harness agents"
elif grep -Eqi 'rm[[:space:]]+-[^[:space:]]*r[^[:space:]]*f[^[:space:]]*[[:space:]]+(/|~|\$HOME)([[:space:]]|$)' <<<"$COMMAND"; then
  reason="broad recursive deletion of root/home is blocked"
elif grep -Eqi 'git[[:space:]]+push([^;&|]*)--force(-with-lease)?' <<<"$COMMAND"; then
  reason="force push is blocked"
elif grep -Eqi 'git[[:space:]]+reset[[:space:]]+--hard' <<<"$COMMAND"; then
  reason="git reset --hard is blocked"
elif grep -Eqi 'git[[:space:]]+clean[[:space:]]+-[^[:space:]]*(x|f)[^[:space:]]*' <<<"$COMMAND"; then
  reason="destructive git clean is blocked"
fi

[[ -z "$reason" ]] && exit 0

if [[ "$EVENT" == "PreToolUse" ]]; then
  jq -n --arg reason "$reason" '{hookSpecificOutput:{hookEventName:"PreToolUse",permissionDecision:"deny",permissionDecisionReason:$reason}}'
  exit 0
fi

if [[ "$EVENT" == "BeforeTool" ]]; then
  jq -n --arg reason "$reason" '{decision:"deny",reason:$reason}'
  exit 0
fi

echo "$reason" >&2
exit 2
