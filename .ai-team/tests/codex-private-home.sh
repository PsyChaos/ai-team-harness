#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fixture="$(mktemp -d -t codex-home-fixture.XXXXXXXX)"
runtime="$(mktemp -d -t codex-runtime-fixture.XXXXXXXX)"
trap 'rm -rf -- "$fixture" "$runtime"' EXIT

mkdir -m 700 "$fixture/source"
printf '{"tokens":{"access_token":"test-only"}}\n' > "$fixture/source/auth.json"
printf 'model = "must-not-be-copied"\n' > "$fixture/source/config.toml"
printf 'private history\n' > "$fixture/source/history.jsonl"
mkdir "$fixture/source/sessions" "$fixture/source/skills"
chmod 600 "$fixture/source/auth.json"

private_home="$(CODEX_HOME="$fixture/source" "$ROOT/.ai-team/bin/prepare-codex-home" "$runtime" implementer)"
[[ "$private_home" == "$runtime/"* ]]
[[ -d "$private_home" && ! -L "$private_home" ]]
[[ "$(stat -c %a "$private_home")" == 700 ]]
[[ "$(stat -c %a "$private_home/auth.json")" == 400 ]]
[[ "$(stat -c %a "$private_home/config.toml")" == 400 ]]
[[ -d "$private_home/sqlite" && "$(stat -c %a "$private_home/sqlite")" == 700 ]]
cmp "$fixture/source/auth.json" "$private_home/auth.json"
[[ ! -e "$private_home/history.jsonl" ]]
[[ ! -e "$private_home/sessions" ]]
[[ ! -e "$private_home/skills" ]]
! grep -q must-not-be-copied "$private_home/config.toml"
grep -q '^default_permissions = "harness-worker"$' "$private_home/config.toml"
grep -q '^":minimal" = "read"$' "$private_home/config.toml"
grep -q '^"\." = "write"$' "$private_home/config.toml"
grep -q '^"\.git" = "write"$' "$private_home/config.toml"
grep -Fq "\"$private_home\" = \"deny\"" "$private_home/config.toml"
[[ -d "$private_home/project-config" ]]

review_home="$(CODEX_HOME="$fixture/source" "$ROOT/.ai-team/bin/prepare-codex-home" "$runtime" reviewer)"
grep -q '^"\." = "read"$' "$review_home/config.toml"
grep -q '^"\.git" = "read"$' "$review_home/config.toml"

if CODEX_HOME="$fixture/source" "$ROOT/.ai-team/bin/prepare-codex-home" "$runtime" invalid-role >/dev/null 2>&1; then
  echo "invalid role unexpectedly accepted" >&2
  exit 1
fi
printf 'PASS: disposable Codex home has least-privilege tool permissions and isolated state\n'
