# Optional Decision Engine

## Principle

**Jev judges; Harness governs.**

The decision engine is optional.

```text
HARNESS_DECISION_ENGINE=rules
```

uses deterministic fallback rules only.

```text
HARNESS_DECISION_ENGINE=jev
```

uses TypeSafe Jev for fuzzy routing and falls back to rules when:

- the API is unavailable
- the API key is absent/invalid
- returned confidence is below the configured threshold
- a routing capability is disabled

## Jev use cases

Allowed:

- task/work-type classification
- provider routing
- model capability profile: fast / balanced / strong
- standard/security/dual review routing
- retry / provider-switch / replan / task-split judgment
- next-action selection among actions already declared valid

Not allowed:

- CI pass/fail
- GitHub dependency completion
- worker/systemd liveness
- branch existence
- PR merge state
- final merge gate

These are deterministic facts and must be queried directly.

## Native TypeSafe API

The harness intentionally uses TypeSafe's native System One HTTP endpoint through
Python's standard library. Jev therefore adds no mandatory Python package.

Runtime variables:

```bash
HARNESS_DECISION_ENGINE="jev"
TYPESAFE_MODEL="jev-latest"
TYPESAFE_BASE_URL="https://api.typesafe.ai"
```

`TYPESAFE_API_KEY` is stored only in the external `HARNESS_SECRETS_FILE` and is
loaded by the privileged `decide` wrapper when Jev is enabled.

The request model is:

```json
{
  "model": "jev-latest",
  "state": {},
  "questions": {
    "provider": {
      "type": "choice",
      "instructions": "...",
      "criteria": {
        "codex": "...",
        "claude": "..."
      }
    }
  }
}
```

The engine consumes the returned `choice`, `probabilities` and `confidence`.

## Confidence gates

Defaults:

```text
work type      0.70
provider       0.75
model profile  0.75
review mode    0.80
retry          0.80
action         0.85
```

These are initial operational defaults, not claims about universal calibration.
Tune them using your own labelled task history.

## Commands

Task routing:

```bash
.ai-team/bin/decide task --state-file state.json
```

Retry routing:

```bash
.ai-team/bin/decide retry --state-file retry.json
```

Action routing:

```bash
.ai-team/bin/decide action --state-file action.json
```

Connection test:

```bash
.ai-team/bin/decide smoke-test
```

## Security

Never put the TypeSafe API key in committed files.

Use:

```text
.ai-team/runtime/runtime.env
```

or an external environment/secret store.

Task state sent to Jev may contain source/task context. Do not include secrets,
credentials or unnecessary sensitive data.
