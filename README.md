# AI Team Harness v1.1.0

> Security architecture: the coordinator is a deterministic broker, not a
> Claude/Codex/Gemini session. Provider models are worker-only. See
> `.ai-team/docs/COORDINATOR-BROKER.md`.

A GitHub-centered, provider-agnostic autonomous software team harness built from
existing CLIs and operating primitives rather than a new orchestration product.

Start from a natural-language project brief, without manually creating issues:

```bash
.ai-team/bin/bootstrap-project --brief PROJECT.md --start
```

The planner creates a bounded plan; the publisher creates/reuses a GitHub Project,
publishes phases/tasks/subtasks with native parent/dependency relationships and
initial provider/model routing, then releases executable work to the coordinator.
See [Usage](USAGE.md) for prerequisites, recovery and continuous operation.

Current v1.2 automation uses a deterministic broker for dispatch, stale-worker
reconciliation, PR publication, independent review, fresh CI/head merge gates,
merge, DONE, cleanup, and native dependency unlock.

## What changed in v1.2

- Privileged provider coordinators were removed; cycles use a deterministic signed broker.
- Secrets moved from runtime.env to an external permission-checked store.
- READY dispatch now uses fresh-state, dependency, capacity and canonical-path gates.
- Workers use isolated local clones with no shared Git metadata; reviewers see them read-only.
- Lifecycle actions and review/merge evidence are head-bound and HMAC-authenticated.

## What changed in v1.1

- Optional TypeSafe Jev decision engine.
- Installer asks whether Jev should be enabled.
- Jev can route task type, provider, model capability profile, review mode, retry strategy and bounded next-action choices.
- Confidence-gated rules fallback.
- CI/dependency/worker/merge facts remain deterministic.
- Claude planner/worker sessions use OAuth-compatible `--safe-mode`; role skills are injected directly into prompts.
- Jev integration uses the native System One HTTP API and requires no mandatory SDK package.

## Principle

> Jev judges; Harness governs.

Jev is never required.

```text
decision_engine=rules
```

works entirely without TypeSafe.

## Runtime

```text
GitHub Project
      |
fresh coordinator cycle
      |
optional Jev/rules decision layer
      |
provider/model/review routing
      |
isolated worker clones
      |
independent review
      |
deterministic CI + merge gates
```

Start with `QUICKSTART-TR.md` or `INSTALL.md`.
