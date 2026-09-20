# Coordinator Command Reference

The deterministic broker is the only coordinator. Provider models must not use
these commands. Normal operation is:

```bash
.ai-team/bin/coordinator-cycle
```

See `COORDINATOR-BROKER.md` for signed snapshot/apply diagnostics.

## GitHub Project field setup

```bash
.ai-team/bin/setup-github-project
```

## Update Project field

Project-field mutation is a private broker operation. Direct calls to
`project-set-field` fail closed.

## Decision engine

```bash
.ai-team/bin/decide task --state-file /tmp/task-state.json
.ai-team/bin/decide retry --state-file /tmp/retry-state.json
.ai-team/bin/decide action --state-file /tmp/action-state.json
```

The decision engine is advisory/judgment only. Deterministic policy gates remain code/GitHub/systemd driven.

## Spawn agent

`spawn-agent` is broker-internal and accepts only canonical registered issue
isolated clones and canonical Task Pack paths after signed bootstrap verification.

## Status

```bash
.ai-team/bin/status
```

## Doctor

```bash
.ai-team/bin/harness-doctor
.ai-team/bin/harness-doctor --live
```
