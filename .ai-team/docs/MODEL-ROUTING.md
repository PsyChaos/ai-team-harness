# Provider / Model Routing

Routing has two layers:

```text
Decision layer
  -> Jev (optional) or rules
  -> provider + capability profile + review mode

Governance layer
  -> provider enabled?
  -> capacity available?
  -> retry policy?
  -> security policy?
  -> task dependency ready?
  -> budget/availability constraints?
```

The governance layer always wins.

## Model profiles

`fast`
- routine low-risk changes
- docs/simple refactors
- speed/cost favored

`balanced`
- normal engineering work
- default implementation profile

`strong`
- architecture
- high/critical risk
- concurrency/state
- security
- data integrity
- difficult debugging

The harness intentionally does not hard-code vendor model names for these profiles.
Provider/model catalogs change. Configure explicit models in `runtime.env` when desired.

## Cross-provider review

Prefer:

```text
Codex implementation -> Claude review
Claude implementation -> Codex review
Gemini implementation -> Claude/Codex review
```

This is a diversity preference, not a quality ranking.

## Jev

When enabled, Jev can choose:

- provider among currently available providers
- model capability profile
- review level

Low-confidence decisions fall back to deterministic rules.

See `DECISION-ENGINE.md`.


## Mapping profiles to concrete models

Optional runtime settings:

```bash
HARNESS_CODEX_MODEL_FAST=""
HARNESS_CODEX_MODEL_BALANCED=""
HARNESS_CODEX_MODEL_STRONG=""

HARNESS_CLAUDE_MODEL_FAST=""
HARNESS_CLAUDE_MODEL_BALANCED=""
HARNESS_CLAUDE_MODEL_STRONG=""

HARNESS_GEMINI_MODEL_FAST=""
HARNESS_GEMINI_MODEL_BALANCED=""
HARNESS_GEMINI_MODEL_STRONG=""
```

When routing returns `model_profile=fast|balanced|strong`, the coordinator passes
that profile to `spawn-agent`.

Resolution order:

```text
profile-specific provider model
-> generic HARNESS_<PROVIDER>_MODEL
-> provider CLI default/current model
```
