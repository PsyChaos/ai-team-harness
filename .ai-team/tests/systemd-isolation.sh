#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
command -v bwrap >/dev/null
runtime_dir="/run/user/$(id -u)"
state_home="$(mktemp -d -t ai-harness-state.XXXXXXXX)"
shared_root="$state_home/ai-team-harness"
secret_dir="$shared_root/repo-a"
sibling_dir="$shared_root/repo-b"
allowed_dir="$state_home/allowed-worktree"
result_file="$state_home/worker-result.md"
host_canary="$ROOT/.ai-team/runtime/isolation-host-canary-$$"
other_task_canary="$ROOT/.ai-team/runtime/issue-999-job-canary-$$"
main_git_config="$ROOT/.git/config"
mkdir -p "$secret_dir" "$sibling_dir" "$allowed_dir"
unit="ai-harness-isolation-test-$$"
trap 'rm -rf -- "$state_home"; rm -f -- "$host_canary" "$other_task_canary"; systemctl --user reset-failed "$unit.service" >/dev/null 2>&1 || true' EXIT
printf 'must-stay-on-host\n' > "$secret_dir/secrets.env"
printf 'sibling-must-stay-hidden\n' > "$sibling_dir/secrets.env"
printf 'empty\n' > "$result_file"
printf 'host-must-stay-unchanged\n' > "$host_canary"
printf 'other-task-must-stay-unchanged\n' > "$other_task_canary"
git_config_digest="$(sha256sum "$main_git_config" | cut -d' ' -f1)"
chmod 700 "$shared_root" "$secret_dir" "$sibling_dir"
chmod 600 "$secret_dir/secrets.env" "$sibling_dir/secrets.env"

set +e
systemd-run --user --wait --collect --pipe --unit="$unit" \
  --property=NoNewPrivileges=yes \
  --property=UnsetEnvironment="HARNESS_BOOTSTRAP_HMAC_KEY HARNESS_BROKER_HMAC_KEY TYPESAFE_API_KEY GH_TOKEN GITHUB_TOKEN DBUS_SESSION_BUS_ADDRESS XDG_RUNTIME_DIR" \
  "$(command -v bwrap)" \
    --ro-bind / / \
    --dev-bind /dev /dev \
    --tmpfs "$shared_root" \
    --tmpfs "$runtime_dir" \
    --tmpfs /tmp \
    --bind "$allowed_dir" "$allowed_dir" \
    --bind "$result_file" "$result_file" \
    --unshare-pid \
    --unshare-uts \
    --unshare-ipc \
    --proc /proc \
    --new-session \
    --die-with-parent \
    "$ROOT/.ai-team/tests/isolation-probe.sh" \
      "$secret_dir/secrets.env" "$sibling_dir/secrets.env" \
      "$allowed_dir" "$result_file" "$host_canary" "$other_task_canary" "$main_git_config"
rc=$?
set -e

[[ "$rc" -eq 0 ]]
[[ "$(cat "$secret_dir/secrets.env")" == must-stay-on-host ]]
[[ "$(cat "$sibling_dir/secrets.env")" == sibling-must-stay-hidden ]]
[[ "$(cat "$allowed_dir/worker.txt")" == worktree-write ]]
[[ "$(cat "$result_file")" == result-write ]]
[[ "$(cat "$host_canary")" == host-must-stay-unchanged ]]
[[ "$(cat "$other_task_canary")" == other-task-must-stay-unchanged ]]
[[ "$(sha256sum "$main_git_config" | cut -d' ' -f1)" == "$git_config_digest" ]]
printf 'PASS: worker writes only allowed outputs, hides secrets, and denies nested systemd\n'
