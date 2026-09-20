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

The worker sandbox on 2026-09-20 reports Codex missing from PATH, Claude Code
2.1.84 unauthenticated, and Gemini missing. Direct `node` execution of the installed
Codex package's `bin/codex.js --version` confirms 0.154.0; `login --help` confirms
the status subcommand. The expected authenticated Codex/Claude 2.1.235 discovery
must still be run by the coordinator in its normal host environment. No credential
files or sandbox restrictions were modified to obtain a passing result.
