#!/usr/bin/env bash
set -euo pipefail
INPUT="$(cat)"
ROOT="${CLAUDE_PROJECT_DIR:-${GEMINI_PROJECT_DIR:-${PWD}}}"
DIR="$ROOT/.ai-team/runtime/logs"
mkdir -p "$DIR"
jq -c '.' <<<"$INPUT" >> "$DIR/provider-tools.jsonl" || true
exit 0
