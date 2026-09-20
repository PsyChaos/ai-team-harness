#!/usr/bin/env bash
set -euo pipefail

harness_root() {
  git rev-parse --show-toplevel
}

load_runtime() {
  local root env_file
  root="$(harness_root)"
  env_file="$root/.ai-team/runtime/runtime.env"
  if [[ ! -f "$env_file" ]]; then
    echo "Missing $env_file. Copy .ai-team/config/runtime.env.example first." >&2
    return 1
  fi

  # shellcheck disable=SC1090
  source "$env_file"

  # Runtime configuration is intentionally non-secret.  Do not let an inherited
  # shell environment or a legacy runtime.env accidentally widen this trust
  # boundary; privileged entrypoints must call load_secrets explicitly.
  unset HARNESS_BOOTSTRAP_HMAC_KEY HARNESS_BROKER_HMAC_KEY TYPESAFE_API_KEY
  unset GH_TOKEN GITHUB_TOKEN

  if [[ -n "${HARNESS_PATH_PREFIX:-}" ]]; then
    export PATH="${HARNESS_PATH_PREFIX}:$PATH"
  fi

  export HARNESS_ROOT="$root"
  mkdir -p \
    "$root/.ai-team/runtime/jobs" \
    "$root/.ai-team/runtime/results" \
    "$root/.ai-team/runtime/taskpacks" \
    "$root/.ai-team/runtime/reviews" \
    "$root/.ai-team/runtime/logs"
}

harness_default_secrets_file() {
  local root state_home repo_id
  root="$(harness_root)"
  state_home="${XDG_STATE_HOME:-$HOME/.local/state}"
  repo_id="$(printf '%s' "$root" | sha256sum | awk '{print $1}')"
  printf '%s/ai-team-harness/%s/secrets.env\n' "$state_home" "$repo_id"
}

load_secrets() {
  local secret_file secret_dir owner mode
  secret_file="${HARNESS_SECRETS_FILE:-$(harness_default_secrets_file)}"
  [[ "$secret_file" = /* ]] || {
    echo "HARNESS_SECRETS_FILE must be an absolute path" >&2
    return 1
  }
  [[ ! "$secret_file" =~ [[:space:]] ]] || {
    echo "HARNESS_SECRETS_FILE must not contain whitespace" >&2
    return 1
  }
  [[ -f "$secret_file" && ! -L "$secret_file" ]] || {
    echo "Missing secure secret store: $secret_file (run .ai-team/bin/migrate-secrets)" >&2
    return 1
  }
  secret_dir="$(dirname "$secret_file")"
  [[ -d "$secret_dir" && ! -L "$secret_dir" ]] || {
    echo "Invalid secure secret-store directory: $secret_dir" >&2
    return 1
  }
  owner="$(stat -c '%u' "$secret_file")"
  mode="$(stat -c '%a' "$secret_file")"
  [[ "$owner" == "$(id -u)" && "$mode" == "600" ]] || {
    echo "Secret store must be owned by the current user with mode 0600" >&2
    return 1
  }
  owner="$(stat -c '%u' "$secret_dir")"
  mode="$(stat -c '%a' "$secret_dir")"
  [[ "$owner" == "$(id -u)" && "$mode" == "700" ]] || {
    echo "Secret-store directory must be owned by the current user with mode 0700" >&2
    return 1
  }
  local name value
  while IFS='=' read -r name value; do
    [[ -z "$name" ]] && continue
    case "$name" in
      HARNESS_BOOTSTRAP_HMAC_KEY|HARNESS_BROKER_HMAC_KEY|TYPESAFE_API_KEY) ;;
      *) echo "Secret store contains an unsupported key" >&2; return 1 ;;
    esac
    [[ "$value" =~ ^[A-Za-z0-9_./:+@=-]*$ ]] || {
      echo "Secret store contains an unsafe value encoding" >&2
      return 1
    }
    printf -v "$name" '%s' "$value"
    export "$name"
  done < "$secret_file"
  HARNESS_SECRETS_FILE="$secret_file"
  export HARNESS_SECRETS_FILE
}

require_var() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    echo "Required runtime variable is empty: $name" >&2
    exit 2
  fi
}

provider_enabled() {
  case "$1" in
    claude) [[ "${HARNESS_ENABLE_CLAUDE:-0}" == "1" ]] ;;
    codex) [[ "${HARNESS_ENABLE_CODEX:-0}" == "1" ]] ;;
    gemini) [[ "${HARNESS_ENABLE_GEMINI:-0}" == "1" ]] ;;
    *) return 1 ;;
  esac
}
