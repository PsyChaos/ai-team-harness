# Installation

After installing into a target repository, start from a natural-language brief:

```bash
.ai-team/bin/bootstrap-project --brief PROJECT.md --start
```

Bootstrap creates/reuses the GitHub Project and creates the work graph; manual
issue setup is optional. Clear `HARNESS_PROJECT_NUMBER` when you want automatic
Project creation. The installer copies the bootstrap command, prompt and Python
publisher with `.ai-team/`. Reinstall to update an existing target checkout; the
installer preserves runtime.env. See [Usage](USAGE.md) for recovery and timer limits.
That preserved file also holds `HARNESS_BOOTSTRAP_HMAC_KEY`, generated at first
bootstrap. Keep a private backup; deleting or rotating it invalidates existing
signed project plans. Runtime job/log caches remain disposable, the credential
configuration does not.

## Prerequisites

Required:

- Git
- GitHub CLI (`gh`)
- `jq`
- Python 3
- systemd user services
- at least one of Claude Code / Codex CLI / Gemini CLI

For CachyOS/Arch:

```bash
sudo pacman -S git github-cli jq python
```

Authenticate GitHub:

```bash
gh auth login
gh auth refresh -s project
```

## Interactive installation

```bash
./install.sh /absolute/path/to/repository
```

The installer detects provider CLIs and asks:

- legacy planner/provider metadata (the coordinator itself is deterministic)
- whether TypeSafe Jev should be enabled
- TypeSafe API key when Jev is selected

The key is placed only in the external permission-checked secret store.

## Non-interactive installation

Rules only:

```bash
./install.sh /path/to/repo \
  --coordinator claude \
  --decision-engine rules \
  --non-interactive
```

Jev:

```bash
export TYPESAFE_API_KEY='...'

./install.sh /path/to/repo \
  --coordinator claude \
  --decision-engine jev \
  --non-interactive
```

Do not pass API keys as command-line arguments; they may be visible in process/history metadata.

## Configure GitHub state

Edit:

```text
.ai-team/runtime/runtime.env
```

Set:

```bash
HARNESS_PROJECT_OWNER="..."
HARNESS_PROJECT_NUMBER="..."
HARNESS_REPO="owner/repository"
```

Then:

```bash
.ai-team/bin/harness-doctor
.ai-team/bin/setup-github-project
```

With Jev:

```bash
.ai-team/bin/harness-doctor --live
```

## Test

```bash
.ai-team/bin/coordinator-cycle
```

## Continuous mode

```bash
.ai-team/bin/install-systemd
systemctl --user enable --now ai-harness-coordinator.timer
```

Observe:

```bash
journalctl --user -u ai-harness-coordinator.service -f
```
