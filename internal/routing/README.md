# Deterministic tuple selection

`Select(ctx, Input, Policy, Judgment)` is a library boundary for the Go routing
layer. It does not invoke providers, reserve capacity, change GitHub state, or
replace the existing Python broker. `TaskRouter` remains the legacy record
interpretation interface.

The caller supplies the ordered `scheduler.Frontier` and the same coherent
snapshot's time and remaining provider capacity, discovered `registry.Entry`
records, task classifications, operator capability mappings, and observations.
Refresh and reserve before dispatch: a routing result is evidence, not authority.
Missing classifications fail closed. Risk is LOW/MEDIUM/HIGH/CRITICAL; complexity
is small/normal/complex. Work type must match an explicit registry capability.

Policy versions identify the operator's complete configuration, including profile,
role, provider and effort preferences. Profile assignments are explicit assertions
about models; routing never infers capabilities from vendor model names. Models
use registry labels; `CLI default` explicitly means the configured CLI default,
not a pinned vendor model revision. Operators needing a pinned model must use ID.

## Rules

1. HIGH/CRITICAL risk, complex tasks, or security, architecture, concurrency, state,
   data_integrity or debugging work require strong. LOW-risk small/docs tasks use
   fast. Other classified tasks use balanced.
2. Require compatible, enabled, installed, authenticated providers, current health,
   positive remaining capacity and fresh provider/model discovery evidence.
3. Require enabled/available models, matching work type, role and profile, and an
   effort present in both the registry and the policy's profile effort order.
4. When configured, the last retry must switch away from the recorded previous
   provider. Missing previous-provider evidence blocks that retry.
5. Reject exhausted known execution quota, invalid numeric evidence, and candidates
   exceeding any declared task, provider or model budget. Budget amounts supplied
   here must be **remaining** allowances, after spending/reservations. Cost evidence
   must have matching units and periods for every declared limit. Unknown cost
   cannot satisfy a known hard limit; explicit zero is a real limit. Unknown quota
   is permitted absent a known restriction and remains null, never zero or unlimited.
   Nil budget/amount declares no numeric limit; it is not a zero-cost assertion.
6. Preserve the first frontier work with a valid tuple. Rank its choices by fresh
   observed successes minus failures for that model/effort/work type, then configured
   provider order, effort order, and lexical provider/model/effort. Stale observations
   become unknown. Unobserved results have no success/failure evidence. No ordering
   claims cheapest or best, and spare capacity is a constraint rather than a score.

`Result` records the complete issue URL/work ID, role, provider, model, effort,
capability profile, cost/quota evidence, decision source, reason, policy version,
and exclusions. Empty selection has an explicit reason. IDs such as `choice-1`
are local to a selection call, not persistent tuple identities.

## Optional judgment

A Jev wrapper can implement `Judgment.Rank` using only the supplied validated
candidate list and return `{candidate_id, confidence, reason}`. `DecodeJudgment`
accepts one strict JSON document of at most 4096 bytes. The response must choose an
existing ID, provide a finite probability meeting policy confidence, and a nonempty
reason of at most 2048 bytes. The adapter cannot change a tuple or skip scheduler
priority. Its candidate evidence is copied to prevent mutations affecting selection.

Timeout/cancellation, malformed replies, low confidence, errors, invalid IDs and
missing adapters return the exact deterministic choice with source `rules_fallback`,
`fallback_reason` and an explicit fallback in `reason`. Callers must persist/display
these fields to make fallback audible; this library does not write global logs.
Successful judgment returns source `judgment`. No candidates returns source `rules`.

The deadline is positive and capped at 30 seconds. Select bounds its wait even if
an adapter ignores cancellation, but cannot kill arbitrary Go code. Adapters must
honor cancellation, terminate their subprocesses and bound output; otherwise a
misbehaving adapter can leave a goroutine running. No production CLI adapter or live
Jev/provider invocation is introduced here.

## Validation

`go test ./internal/routing/...` runs table-driven fake discovery/adapter tests for
small/simple, high-risk, scarce-capacity, disabled-model, unsupported-effort,
stale-health, provider-outage and retry/provider-switch. Additional checks exercise
budgets/unknown values, fresh history, security work, frontier order, input-order
invariance, adapter mutation, strict decoding and all judgment fallbacks.

Local implementation validation on 2026-09-21:

- `go test ./internal/routing/...`: passed (0.045s).
- `go build ./...`: passed, exit 0.
- `go vet ./...`: passed, exit 0.
- `go test -race ./...`: passed for all packages; routing passed (1.068s).
- Fixtures use no live provider or judgment calls. Hosted CI remains pending the
  coordinator's publication and independent review; local checks are not hosted CI.
