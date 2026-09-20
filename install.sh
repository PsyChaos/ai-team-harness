#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  ./install.sh /path/to/repository [options]

Options:
  --coordinator claude|codex|gemini
  --decision-engine rules|jev
  --non-interactive
  --skip-jev-test
  -h, --help

In non-interactive mode, Jev requires TYPESAFE_API_KEY in the environment when
--decision-engine jev is selected. The key is never accepted as a CLI argument.
EOF
}

[[ "$#" -ge 1 ]] || { usage >&2; exit 2; }

TARGET=""
COORDINATOR=""
DECISION_ENGINE=""
NON_INTERACTIVE=0
SKIP_JEV_TEST=0

while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --coordinator)
      COORDINATOR="${2:-}"; shift 2 ;;
    --decision-engine)
      DECISION_ENGINE="${2:-}"; shift 2 ;;
    --non-interactive)
      NON_INTERACTIVE=1; shift ;;
    --skip-jev-test)
      SKIP_JEV_TEST=1; shift ;;
    -h|--help)
      usage; exit 0 ;;
    -*)
      echo "Unknown option: $1" >&2; usage >&2; exit 2 ;;
    *)
      if [[ -n "$TARGET" ]]; then
        echo "Only one target repository may be supplied." >&2
        exit 2
      fi
      TARGET="$1"; shift ;;
  esac
done

[[ -n "$TARGET" ]] || { echo "Target repository is required." >&2; exit 2; }

SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
[[ -d "$TARGET" ]] || { echo "Target directory not found: $TARGET" >&2; exit 3; }
DST="$(cd "$TARGET" && pwd)"

if ! git -C "$DST" rev-parse --show-toplevel >/dev/null 2>&1; then
  echo "Target is not inside a Git repository: $DST" >&2
  exit 4
fi
DST="$(git -C "$DST" rev-parse --show-toplevel)"

echo "AI Team Harness v1.2.0"
echo "Target: $DST"
echo

# Preserve local runtime config on upgrades.
RUNTIME_BACKUP=""
if [[ -f "$DST/.ai-team/runtime/runtime.env" ]]; then
  RUNTIME_BACKUP="$(mktemp)"
  cp "$DST/.ai-team/runtime/runtime.env" "$RUNTIME_BACKUP"
fi

mkdir -p "$DST/.ai-team" "$DST/.agents"
cp -a "$SRC/.ai-team/." "$DST/.ai-team/"
cp -a "$SRC/.agents/." "$DST/.agents/"

mkdir -p "$DST/.ai-team/runtime"
touch "$DST/.ai-team/runtime/.gitkeep"
if [[ -n "$RUNTIME_BACKUP" ]]; then
  cp "$RUNTIME_BACKUP" "$DST/.ai-team/runtime/runtime.env"
  chmod 600 "$DST/.ai-team/runtime/runtime.env"
  rm -f "$RUNTIME_BACKUP"
  echo "preserved: .ai-team/runtime/runtime.env"
fi

copy_if_absent() {
  local rel="$1"
  if [[ -e "$DST/$rel" || -L "$DST/$rel" ]]; then
    cp -a "$SRC/$rel" "$DST/$rel.ai-harness.new"
    echo "kept existing: $rel"
    echo "  review: $rel.ai-harness.new"
  else
    mkdir -p "$DST/$(dirname "$rel")"
    cp -a "$SRC/$rel" "$DST/$rel"
    echo "installed: $rel"
  fi
}

copy_if_absent "AGENTS.md"
copy_if_absent "CLAUDE.md"
copy_if_absent "GEMINI.md"
copy_if_absent ".github/ISSUE_TEMPLATE/ai-task.md"
copy_if_absent ".github/PULL_REQUEST_TEMPLATE.md"
copy_if_absent ".github/workflows/harness-ci.yml"
copy_if_absent ".gemini/settings.json"
copy_if_absent ".claude/settings.json"

# Claude skill symlinks. --bare runtime does not depend on discovery, but these
# remain useful for interactive/manual Claude sessions.
mkdir -p "$DST/.claude/skills"
for skilldir in "$DST"/.agents/skills/*; do
  [[ -d "$skilldir" ]] || continue
  name="$(basename "$skilldir")"
  target="$DST/.claude/skills/$name"
  [[ -e "$target" || -L "$target" ]] || ln -s "../../.agents/skills/$name" "$target"
done

touch "$DST/.gitignore"
for line in \
  ".ai-team/runtime/*" \
  "!.ai-team/runtime/.gitkeep" \
  ".worktrees/" \
  "*.provider-output" \
  "__pycache__/" \
  "*.py[cod]"
do
  grep -Fxq "$line" "$DST/.gitignore" || echo "$line" >> "$DST/.gitignore"
done

chmod +x "$DST"/.ai-team/bin/* "$DST"/.ai-team/hooks/*.sh "$DST"/.ai-team/decision/*.py

ENV_FILE="$DST/.ai-team/runtime/runtime.env"
if [[ ! -f "$ENV_FILE" ]]; then
  cp "$DST/.ai-team/config/runtime.env.example" "$ENV_FILE"
  chmod 600 "$ENV_FILE"
  echo "created: .ai-team/runtime/runtime.env"
fi

set_env() {
  local key="$1"
  local value="$2"
  local tmp
  tmp="$(mktemp)"
  grep -v "^${key}=" "$ENV_FILE" > "$tmp" || true
  printf '%s=%q\n' "$key" "$value" >> "$tmp"
  mv "$tmp" "$ENV_FILE"
  chmod 600 "$ENV_FILE"
}

detect_provider() {
  local p="$1"
  command -v "$p" >/dev/null 2>&1
}

if [[ "$NON_INTERACTIVE" -eq 0 ]]; then
  echo
  echo "Detected provider CLIs:"
  detect_provider claude && echo "  ✓ Claude Code" || echo "  - Claude Code not found"
  detect_provider codex && echo "  ✓ Codex CLI" || echo "  - Codex CLI not found"
  detect_provider gemini && echo "  ✓ Gemini CLI" || echo "  - Gemini CLI not found"

  if [[ -z "$COORDINATOR" ]]; then
    default_coord="claude"
    detect_provider claude || default_coord="codex"
    read -r -p "Coordinator provider [${default_coord}]: " answer
    COORDINATOR="${answer:-$default_coord}"
  fi

  if [[ -z "$DECISION_ENGINE" ]]; then
    read -r -p "Enable TypeSafe Jev decision engine? [y/N]: " answer
    case "${answer:-N}" in
      y|Y|yes|YES) DECISION_ENGINE="jev" ;;
      *) DECISION_ENGINE="rules" ;;
    esac
  fi
else
  COORDINATOR="${COORDINATOR:-claude}"
  DECISION_ENGINE="${DECISION_ENGINE:-rules}"
fi

case "$COORDINATOR" in claude|codex|gemini) ;; *)
  echo "Invalid coordinator: $COORDINATOR" >&2; exit 5 ;;
esac
case "$DECISION_ENGINE" in rules|jev) ;; *)
  echo "Invalid decision engine: $DECISION_ENGINE" >&2; exit 6 ;;
esac

set_env HARNESS_COORDINATOR_PROVIDER "$COORDINATOR"
set_env HARNESS_DECISION_ENGINE "$DECISION_ENGINE"

if [[ "$DECISION_ENGINE" == "jev" ]]; then
  key="${TYPESAFE_API_KEY:-}"

  if [[ "$NON_INTERACTIVE" -eq 0 && -z "$key" ]]; then
    echo
    echo "Jev is optional and will fall back to rules on API/confidence failure."
    read -r -s -p "TypeSafe API key (leave empty to configure later): " key
    echo
  fi

  if [[ -n "$key" ]]; then
    set_env TYPESAFE_API_KEY "$key"
    (cd "$DST" && "$DST/.ai-team/bin/migrate-secrets" >/dev/null)
    echo "configured: TypeSafe API key in external secret store"

    if [[ "$SKIP_JEV_TEST" -eq 0 ]]; then
      echo "Jev connection test..."
      set +e
      (
        cd "$DST"
        "$DST/.ai-team/bin/decide" smoke-test >/tmp/ai-harness-jev-test.$$ 2>&1
      )
      rc=$?
      set -e
      if [[ "$rc" -eq 0 ]]; then
        echo "  ✓ Jev connection test passed"
      else
        echo "  ! Jev connection test failed; rules fallback remains available"
        sed -n '1,20p' /tmp/ai-harness-jev-test.$$ 2>/dev/null || true
      fi
      rm -f /tmp/ai-harness-jev-test.$$
    fi
  else
    echo "Jev selected but no API key configured."
    echo "Run .ai-team/bin/migrate-secrets, then add TYPESAFE_API_KEY to HARNESS_SECRETS_FILE."
    echo "Until then, use HARNESS_DECISION_ENGINE=rules or configure the key."
  fi
fi

# Move any legacy values out of runtime.env and create the broker/bootstrap
# signing keys before doctor, bootstrap, or a timer can run.
(cd "$DST" && "$DST/.ai-team/bin/migrate-secrets" >/dev/null)
echo "configured: external 0700/0600 secret store"

echo
echo "Harness installed into: $DST"
echo "Coordinator: $COORDINATOR"
echo "Decision engine: $DECISION_ENGINE"
echo
echo "Next:"
echo "  cd \"$DST\""
echo "  \$EDITOR .ai-team/runtime/runtime.env"
echo "  .ai-team/bin/harness-doctor"
echo "  .ai-team/bin/setup-github-project"
echo "  .ai-team/bin/coordinator-cycle"
