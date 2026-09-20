#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
grep -Fq -- 'BWRAP_ARGS+=(--tmpfs "$SHARED_SECRET_ROOT")' \
  "$ROOT/.ai-team/bin/spawn-agent"
grep -Fq -- '--tmpfs "$RUNTIME_DIR"' \
  "$ROOT/.ai-team/bin/spawn-agent"
grep -Fq -- '"$HOME/.config/gh" "$HOME/.ssh"' "$ROOT/.ai-team/bin/spawn-agent"
TEST_ROOT="$(mktemp -d)"
trap 'rm -rf "$TEST_ROOT"' EXIT
mkdir -p "$TEST_ROOT/.ai-team/bin" "$TEST_ROOT/.ai-team/runtime"
cp "$ROOT/.ai-team/bin/lib.sh" "$ROOT/.ai-team/bin/migrate-secrets" "$TEST_ROOT/.ai-team/bin/"
cat > "$TEST_ROOT/.ai-team/runtime/runtime.env" <<'EOF'
HARNESS_REPO="acme/widget"
HARNESS_BOOTSTRAP_HMAC_KEY="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
TYPESAFE_API_KEY="do-not-leak"
EOF
git -C "$TEST_ROOT" init -q
(
  cd "$TEST_ROOT"
  XDG_STATE_HOME="$TEST_ROOT/state" .ai-team/bin/migrate-secrets >/dev/null
)
! grep -Eq 'HARNESS_BOOTSTRAP_HMAC_KEY|HARNESS_BROKER_HMAC_KEY|TYPESAFE_API_KEY' \
  "$TEST_ROOT/.ai-team/runtime/runtime.env"
SECRET_FILE="$(sed -n 's/^HARNESS_SECRETS_FILE=//p' "$TEST_ROOT/.ai-team/runtime/runtime.env")"
[[ "$(stat -c '%a' "$SECRET_FILE")" == 600 ]]
[[ "$(stat -c '%a' "$(dirname "$SECRET_FILE")")" == 700 ]]
(
  cd "$TEST_ROOT"
  export HARNESS_BOOTSTRAP_HMAC_KEY=inherited TYPESAFE_API_KEY=inherited GH_TOKEN=inherited
  # shellcheck disable=SC1091
  source .ai-team/bin/lib.sh
  load_runtime
  [[ -z "${HARNESS_BOOTSTRAP_HMAC_KEY:-}" && -z "${TYPESAFE_API_KEY:-}" && -z "${GH_TOKEN:-}" ]]
  load_secrets
  [[ "$TYPESAFE_API_KEY" == do-not-leak ]]
)

# A fresh legacy/manual repo gets only the broker key. Signed-only mode is an
# explicit bootstrap transition, and a lost active key is never regenerated.
FRESH="$TEST_ROOT/fresh"
mkdir -p "$FRESH/.ai-team/bin" "$FRESH/.ai-team/runtime"
cp "$ROOT/.ai-team/bin/lib.sh" "$ROOT/.ai-team/bin/migrate-secrets" "$FRESH/.ai-team/bin/"
printf 'HARNESS_REPO="acme/legacy"\n' > "$FRESH/.ai-team/runtime/runtime.env"
git -C "$FRESH" init -q
(
  cd "$FRESH"
  XDG_STATE_HOME="$TEST_ROOT/fresh-state" .ai-team/bin/migrate-secrets >/dev/null
  fresh_secret="$(sed -n 's/^HARNESS_SECRETS_FILE=//p' .ai-team/runtime/runtime.env)"
  ! grep -q '^HARNESS_BOOTSTRAP_HMAC_KEY=' "$fresh_secret"
  .ai-team/bin/migrate-secrets --bootstrap >/dev/null
  grep -q '^HARNESS_BOOTSTRAP_HMAC_KEY=' "$fresh_secret"
  grep -q '^HARNESS_BOOTSTRAP_KEY_STATE=active$' .ai-team/runtime/runtime.env
  grep -v '^HARNESS_BOOTSTRAP_HMAC_KEY=' "$fresh_secret" > "$fresh_secret.tmp"
  mv "$fresh_secret.tmp" "$fresh_secret"
  chmod 600 "$fresh_secret"
  ! .ai-team/bin/migrate-secrets --bootstrap >/dev/null 2>&1
)
printf 'PASS: runtime config is secret-free and privileged loading is permission-checked\n'
