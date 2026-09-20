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

### Supplied host report (source commit only)

The issue #5 retry Task Pack supplies an independent host report titled
“Issue #5 independent host validation”, attributed to OpenAI Codex subagent
`/root/review_final_refactor`. It reports PASS on the authenticated coordinator
host on 2026-09-20 for source commit
`f0403d0c7c6f7fecb853c2efecf0940b6e6276aa`, with a clean checkout before and after.
This is supplied evidence, not a host run performed by this retry worker.

The report records successful `go build -o /tmp/issue-5-host-discovery ./cmd/harness`,
`go test ./internal/registry/...` (`0.032s`),
`/tmp/issue-5-host-discovery discover .ai-team/config/registry.json` (exit 0),
and `git diff --check`, using Go 1.27.0.

| Provider | Reported version | Installed | Auth | Available | Reported discovery time (UTC) |
| --- | --- | --- | --- | --- | --- |
| Codex | codex-cli 0.154.0 | true | authenticated | true | 2026-09-20T20:49:43.498458352Z |
| Claude | 2.1.235 (Claude Code) | true | authenticated | true | 2026-09-20T20:49:43.633043043Z |
| Gemini | empty | false | unknown | false | 2026-09-20T20:49:44.014529188Z |

Reported executable paths were
`/home/heisenberg/.nvm/versions/node/v25.8.1/bin/codex` and
`/home/heisenberg/.local/bin/claude`; Gemini was absent from PATH. The report
records only version/auth-status probes, CLI-default labels without model IDs
or efforts, and null budgets. No model work or Gemini invocation was reported.

This supersedes the old documentation's statement that no authenticated host
check had been performed. It does **not** establish canonical registration: the
rejecting review found that the broker's canonical external-validation field
said no report was registered. This worker cannot verify the supplied report's
signature or repair that broker field. The broker must register and verify host
evidence for the submitted HEAD before marking the live-host criterion complete.
The source-commit report above does not directly validate a later documentation
commit, even though this retry changes no implementation or config.

### Retry worker checks

Against the source commit above, this worker ran the following commands with
`GOROOT=/home/heisenberg/.goenv/versions/1.27.0`,
`GOCACHE=/tmp/issue-5-retry-go-cache` and
`GOMODCACHE=/tmp/issue-5-retry-go-mod`, using that GOROOT's `bin/go`:

- `go test ./internal/registry/...` — exit 0, `ok` (`0.024s`).
- `go build -o /tmp/issue-5-retry-discovery ./cmd/harness` — exit 0.
- `/tmp/issue-5-retry-discovery discover .ai-team/config/registry.json` — exit 0;
  loaded all three providers, labeled all models `CLI default`, omitted model IDs,
  preserved empty efforts and null budgets.

The retry discovery timestamps span 2026-09-20T21:43:25.452251915Z through
2026-09-20T21:43:26.257442651Z. This sandbox sees Codex and Gemini missing from
PATH, and `/usr/bin/claude` version 2.1.84 unauthenticated; all are unavailable.
These results demonstrate fail-closed discovery here, not the required host
availability. They describe a different environment from the supplied host report.
No credentials or sandbox restrictions were changed. Hosted CI and canonical
host-evidence registration remain external handoff requirements.
