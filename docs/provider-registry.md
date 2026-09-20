# Provider capability registry

Run `go run ./cmd/harness discover .ai-team/config/registry.json` for a read-only
JSON discovery report. The Python executor remains unchanged. No model requests,
login flows, scheduling, quota lookups or GitHub operations occur.

The config is strict JSON. Each provider declares `name` (codex, claude, gemini),
`cli` (executable name on PATH or absolute path), `enabled`, `concurrency`,
optional `budget`, and `models`. Concurrency is a nonnegative configured limit;
discovery availability is independent of scheduler capacity. Each model declares
exactly one of `id` or `cli_default: true`, plus `enabled`, `efforts`, `work_types`
and optional `budget`. Configured IDs and efforts are operator assertions, not
inferred tool support. Empty efforts mean none explicitly verified/configured.
The supplied config uses only entries labeled **CLI default**, with no model ID.
Work types and concurrency in that file are explicit operator policy defaults.

Budgets use `amount`, `unit` and `period`, e.g.
`{"amount":10,"unit":"USD","period":"day"}`. Missing budget/amount is null
(unknown), distinct from an explicit zero. No prices, quotas or usage are inferred.

Discovery checks executable presence, `--version`, then `codex login status` or
`claude auth status --json`, with a five-second deadline per command. Authentication
must be positively recognized and exit successfully. Failure or unfamiliar output
keeps the provider unavailable. Raw auth output is discarded. Gemini has no
verified auth adapter yet and remains unavailable even if installed. Provider
entries record UTC discovery time, config source, executable path and probe names;
model entries record discovery time and explicit config provenance. Refresh before
using these observations: this report is not a durable authentication guarantee.

## Validation for issue #5

`go test ./internal/registry/...` covers fake Codex 0.154.0 and Claude Code 2.1.235,
authenticated/unauthenticated and malformed auth responses, disabled providers and
models, missing Gemini, cancellation, config validation, CLI-default labels and
unknown budgets versus explicit zero. No test requires provider credentials.

### Host evidence status

The retry Task Pack contains two historical host-report claims attributed to
OpenAI Codex subagent `/root/review_final_refactor`: one for
`f0403d0c7c6f7fecb853c2efecf0940b6e6276aa`, and a newer documentation-correction
report for `b240adc1f48ab00c6bcaa90f90e798586a5ef279`. Both claim authenticated
Codex 0.154.0 and Claude Code 2.1.235, with Gemini absent. The newer report gives
UTC discovery timestamps from 2026-09-20T21:45:25.531594012Z through
2026-09-20T21:45:26.268433791Z.

These are supplied, unverified claims, not observations made by this worker.
The rejecting review reports that the canonical broker field contained no
registered report for that HEAD. Neither a PASS heading nor a statement that a
report is signed establishes broker verification. The supplied reports do not
validate a later commit. Live-host acceptance remains unverified here.

The checked-in broker's `external_validation_report` function accepts evidence
only when its signature, repository, issue, exact HEAD, `kind: browser` and
`outcome: PASS` match. Its operator-only `resume --validation-report` path
registers that evidence and changes coordinator/GitHub state; it also requires
an idle worker and a WAITING_HUMAN implementation. The `browser` name is the
existing external-validation channel, not evidence that browser testing occurred.
This worker has not invoked recovery, registered evidence, or verified a
signature. Copying report prose into documentation cannot satisfy that mechanism.
Evidence registration and any required host rerun belong to the operator/broker
handoff after the final commit is known.

### Current retry worker checks

Source checkout: `b240adc1f48ab00c6bcaa90f90e798586a5ef279`, clean before checks.
The worker used `/home/heisenberg/.goenv/versions/1.27.0/bin/go`, with
`GOROOT=/home/heisenberg/.goenv/versions/1.27.0`,
`GOCACHE=/tmp/issue-5-evidence-go-cache` and
`GOMODCACHE=/tmp/issue-5-evidence-go-mod`:

- `go test ./internal/registry/...` — exit 0, `ok` (`0.025s`).
- `go build -o /tmp/issue-5-evidence-discovery ./cmd/harness` — exit 0.
- `/tmp/issue-5-evidence-discovery discover .ai-team/config/registry.json` —
  exit 0; loaded all three providers and preserved CLI-default labels, omitted
  model IDs, empty efforts and null provider/model budgets.

| Provider | Observed version | Installed | Auth | Available |
| --- | --- | --- | --- | --- |
| Codex | empty | false | unknown | false |
| Claude | 2.1.84 (Claude Code) | true | unauthenticated | false |
| Gemini | empty | false | unknown | false |

Discovery timestamps span 2026-09-20T21:58:30.640770067Z through
2026-09-20T21:58:31.476838636Z (2026-09-21 in Europe/Istanbul). Claude resolved to
`/usr/bin/claude`; Codex and Gemini were absent from PATH. These observations
verify fail-closed sandbox behavior, not the required authenticated-host result.
No credentials or sandbox restrictions were changed. This correction changes
only documentation; it cannot resolve the outstanding evidence-registration
blocker or claim hosted CI for the resulting commit.
