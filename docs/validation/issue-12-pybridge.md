# Issue 12 local validation

Validated on 2026-09-20 in the assigned issue-12 checkout.

| Command | Result |
| --- | --- |
| `go test ./internal/observer/pybridge/...` | PASS, recorded Python snapshot and normalized Project item |
| `go build ./...` | PASS |
| `go vet ./...` | PASS |
| `go test -race ./...` | PASS, all Go packages |
| `git diff --check` | PASS |

Manual command replay used the documented offline invocation, wrapping the
existing `internal/domain/testdata/project-item.json` in a one-element JSON
array at `/tmp/issue-12-pybridge-items.json`:

```sh
go run ./cmd/pybridge -snapshot internal/domain/testdata/snapshot.json \
  -github-items /tmp/issue-12-pybridge-items.json
```

The command persisted `/tmp/pybridge-journal-911341566/journal.json` and printed
version 1, cursor 2, with events 1 (`pybridge.broker.observed`) and 2
(`pybridge.task.observed`). The broker event contained repository `acme/widget`,
created-at `1700000000`, expires-at `1700000300` and 9 proposed actions. The task
event contained issue 7, OPEN/READY, Provider OpenAI, Agent Role Implementer,
and a complete dependency list containing closed issue 4. A Python validation
wrapper asserted these values and SHA-256 equality before/after for all three
source files. All assertions passed. No live GitHub calls were made.

This was a recorded fixture replay, **not** the requested real-current-snapshot
manual check. `.ai-team/runtime` contains only `.gitkeep`, and no current broker
snapshot was provided. That check remains unavailable locally. Hosted CI also
remains pending; the implementer did not push or query CI. The coordinator must
run the documented live invocation against an existing current snapshot and
readable Project, verify source hashes and corresponding journal observations,
and obtain hosted CI evidence before the task is considered fully validated.
