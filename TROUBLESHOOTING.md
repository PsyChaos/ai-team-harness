# Troubleshooting

## Upgrade reports an invalid or missing secret store

Run:

```bash
.ai-team/bin/migrate-secrets
.ai-team/bin/harness-doctor
```

Do not generate a replacement bootstrap HMAC key when signed roots already
exist. Restore the original external secret-store backup; otherwise v1 roots
correctly fail signature validation.

## Coordinator does not start Claude/Codex/Gemini

This is expected. `coordinator-cycle` is deterministic and provider-independent.
Models are started only as isolated implementers/reviewers. The deterministic
broker owns dispatch, reconciliation, publication, review, merge, finalization,
and dependency unlock.

## Codex fails to initialize in the worker sandbox

Current workers create a disposable writable `CODEX_HOME` and separate
`CODEX_SQLITE_HOME`; the host `~/.codex` is never made writable. Upgrade the
harness if logs contain `failed to initialize in-process app-server client:
Read-only file system`.

Run the structural check:

```bash
.ai-team/tests/codex-private-home.sh
```

For a bounded real-provider startup and credential-read denial canary:

```bash
HARNESS_RUN_CODEX_SMOKE=1 .ai-team/tests/codex-bwrap-smoke.sh
```

This consumes one small Codex request. It must report that workspace writing
succeeded and model-generated tool execution could not read `auth.json`.

## Jev enabled but doctor fails

Check:

```bash
grep -E 'HARNESS_DECISION_ENGINE|TYPESAFE_' .ai-team/runtime/runtime.env
.ai-team/bin/harness-doctor --live
```

If TypeSafe access is unavailable, temporarily switch to:

```bash
HARNESS_DECISION_ENGINE="rules"
```

The project continues normally.

## Jev returns rules-fallback

This is intentional when:

- confidence is below threshold
- TypeSafe API fails
- provider routing has no usable answer

Inspect manually:

```bash
.ai-team/bin/decide task --state-file .ai-team/examples/decision-task.json
```

## Claude loads personal plugins/MCPs

The harness Claude path should use:

```text
--safe-mode
```

Check:

```bash
grep -- '--safe-mode' .ai-team/bin/coordinator-cycle
grep -- '--safe-mode' .ai-team/bin/run-provider-agent
```

`--bare` must not appear: it disables normal OAuth/keychain authentication.
If `--safe-mode` is missing, you are running an older harness version.

## Claude rejects `gh` or `.ai-team/bin` commands

In non-interactive `-p` mode, `--tools` only exposes tools to Claude; it does not
approve commands. Current harness launchers pair `--permission-mode dontAsk`
with command- and role-scoped `--allowedTools` rules so approved automation runs
without a prompt and every unmatched call fails closed.

Check the installed launcher:

```bash
grep -E -- '--permission-mode|--allowedTools' .ai-team/bin/coordinator-cycle
grep -E -- '--permission-mode|--allowedTools' .ai-team/bin/run-provider-agent
```

Do not replace these rules with bare `Bash` or
`--dangerously-skip-permissions`. Organization-managed deny rules take
precedence over command-line allow rules; if a scoped command is still denied,
ask the Claude administrator to inspect the managed policy.

## Claude says max turns reached

Increase:

```bash
HARNESS_COORDINATOR_MAX_TURNS="80"
```

A tiny manual `--max-turns 3` test can legitimately terminate before a coordinator
cycle finishes.

## GitHub "unknown owner type"

Verify the actual sourced runtime file:

```bash
source .ai-team/runtime/runtime.env
printf 'OWNER=<%s>\nPROJECT=<%s>\nREPO=<%s>\n' \
  "$HARNESS_PROJECT_OWNER" "$HARNESS_PROJECT_NUMBER" "$HARNESS_REPO"
```

Then:

```bash
gh project field-list "$HARNESS_PROJECT_NUMBER" --owner "$HARNESS_PROJECT_OWNER"
```
