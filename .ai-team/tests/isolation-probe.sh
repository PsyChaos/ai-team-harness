#!/usr/bin/env bash
set -euo pipefail
[[ "$#" -ge 5 ]]
[[ ! -e "$1" ]]
[[ ! -e "$2" ]]
printf 'worktree-write\n' > "$3/worker.txt"
printf 'result-write\n' > "$4"
for target in "${@:5}"; do
  if printf 'host-corruption\n' >> "$target" 2>/dev/null; then
    echo "host root unexpectedly writable: $target" >&2
    exit 1
  fi
done
if systemd-run --user --wait --collect /usr/bin/true; then
  echo "nested user-systemd unexpectedly reachable" >&2
  exit 1
fi
