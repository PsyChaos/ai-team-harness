#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TEST_ROOT="$(mktemp -d)"
trap 'rm -rf "$TEST_ROOT"' EXIT
mkdir -p "$TEST_ROOT/.ai-team/bin"
cp "$ROOT/.ai-team/bin/coordinator-cycle" "$TEST_ROOT/.ai-team/bin/"
cat > "$TEST_ROOT/.ai-team/bin/coordinator-broker" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" > "${BROKER_CALLED:?}"
EOF
chmod +x "$TEST_ROOT/.ai-team/bin/"*
for provider in claude codex gemini gh; do
  cat > "$TEST_ROOT/$provider" <<EOF
#!/usr/bin/env bash
echo forbidden > "$TEST_ROOT/provider-called"
exit 99
EOF
  chmod +x "$TEST_ROOT/$provider"
done
BROKER_CALLED="$TEST_ROOT/broker-called" PATH="$TEST_ROOT:$PATH" \
  "$TEST_ROOT/.ai-team/bin/coordinator-cycle"
[[ "$(cat "$TEST_ROOT/broker-called")" == "run" ]]
[[ ! -e "$TEST_ROOT/provider-called" ]]
printf 'PASS: coordinator-cycle invokes only the deterministic broker\n'
