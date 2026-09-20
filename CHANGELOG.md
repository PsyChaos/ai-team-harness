# Changelog

## 1.2.0

- Replace privileged model coordinator sessions with a deterministic signed broker.
- Move bootstrap/broker/Jev secrets into an external 0700/0600 store.
- Add fail-closed READY dispatch, fresh-state/action validation and canonical path gates.
- Scope Claude worker reads to the assigned worktree.

## 1.1.0 - 2026-09-19

- Added optional TypeSafe Jev decision engine.
- Added confidence-gated rules fallback.
- Added task/provider/model-profile/review/retry/action routing.
- Model profiles now resolve to configurable provider-specific FAST/BALANCED/STRONG models.
- Added interactive Jev selection/API-key setup to installer.
- Added native System One HTTP client with no mandatory SDK dependency.
- Added decision examples and documentation.
- Deterministic CI/dependency/worker/merge facts explicitly excluded from Jev.
- Claude coordinator/worker invocations now use `--bare`.
- Role skills are injected directly into provider prompts.
- Doctor validates runtime placeholders and optional live Jev connectivity.

## 1.0.0

Initial GitHub-centered autonomous team harness.
