# Issue #10 local invocation validation

Date: 2026-09-21. Source base: `6455033` (issue #9 routing integration).
This report covers the invocation code and fixtures committed together with it
on branch `ai/issue-10-provider-process-invocation-with-verifie`.

## Local results

| Command | Result |
| --- | --- |
| `go test ./internal/invocation/...` | exit 0; invocation package passed (0.018s) |
| `go test -v ./internal/invocation/...` | exit 0; all four top-level tests passed (0.026s) |
| `go build ./...` | completed without diagnostics |
| `go vet ./...` | completed without diagnostics |
| `go test -race ./...` | exit 0; all 13 packages with tests passed, including invocation (1.038s) |
| `git diff --cached --check` | exit 0; no whitespace errors |

The supported-command test launches a local fake CLI for every one of the 12
supported provider/model/effort combinations and checks exact argv, literal
stdin delivery, the requested/effective result, and its JSON round trip.
The negative fixture rejects Opus 4.6/xhigh with an exact recorded reason and
zero executor calls. Additional checks reject versions, aliases/defaults,
unknown providers/models, empty/invalid effort and injection-shaped strings.
Cancellation, missing executor and process errors preserve honest results.

## Manual CLI comparison

Captured the installed tools' versions and help without authentication or a
model request. See [provenance and captured outputs](../../internal/invocation/testdata/README.md).

For the generated high-effort Codex command, manually compared `exec`,
`--ephemeral`, `--strict-config`, `--model`, `--config` and stdin `-`
against Codex 0.154.0 top-level/exec help. The help documents TOML config values;
the official config reference documents `model_reasoning_effort`. The generated
argument is the single string `model_reasoning_effort="high"`.

For the generated high-effort Claude command, manually compared `--print`,
`--no-session-persistence`, `--output-format text`, `--model`, and
`--effort high` against the available Claude help. They are present, but that
executable reports **2.1.84**. This does not validate the required **2.1.235**
installation. The adapter deliberately rejects 2.1.84; its tests use a fake
executor for the target 2.1.235 contract. Current official documentation
corroborates the selected flags/model efforts, but cannot replace the missing
exact-version host check.

## Acceptance mapping

- [x] Correct command construction for the documented support subset: all 12
  combinations tested against recorded help/config facts and a fake process.
- [x] Unsupported combination fails before spawn with a recorded reason:
  `TestUnsupportedFixtureNeverExecutes`.
- [x] Requested and effective command settings are distinct:
  `TestSupportedInvocations` checks values and JSON serialization.
- [ ] [external:host] Compare generated Claude invocation against installed
  Claude Code 2.1.235 `--version` and `--help`; record actual output and source
  HEAD. Available sandbox CLI is 2.1.84.
- [ ] [external:ci] Hosted CI has not run; coordinator must publish and collect it.

No push, GitHub query, worker credential access, or authenticated model execution
was performed. Effective settings describe command arguments, not remote service
telemetry. Production executor integration and namespace isolation remain outside
this issue. This evidence permits a VALIDATION_PENDING handoff, not merge approval.
