# Go foundation

The repository-root module builds the `cmd/harness` CLI and `internal/` packages
using only the standard library (Go 1.23 or later). The CLI reports migration
status; the Python broker remains the active executor.

- `domain`: existing broker, bootstrap and decision-engine JSON records.
- `ghgateway`: read-only normalized Project item interface.
- `routing`: task routing evidence interface, without dispatch authority.
- `journal`: bounded persisted observations with snapshot and SSE handlers; see
  [the journal contract](event-journal.md).
- `config`, `scheduler`: documented package placeholders. Configuration loading
  and scheduling behavior belong to later tasks.

No provider SDK, credentials, GitHub writes, systemd calls or Python subprocess
bridge are introduced by the Go command. Existing shell entrypoints are unchanged.
Capability profiles in broker actions are not concrete model IDs; the optional
routing result model identifies the judgment engine.

Validate from the repository root:

```sh
go build ./...
go vet ./...
go test -race ./...
python3 -m unittest discover -s .ai-team/tests -p 'test_*.py'
bash .ai-team/tests/secret-boundary.sh
bash .ai-team/tests/codex-private-home.sh
bash .ai-team/tests/coordinator-cycle-claude-args.sh
```

Race tests require a working C compiler. Harness CI installs Go and runs these
checks, fixture regeneration/comparison, and the existing shell syntax checks in
one job. Authenticated provider and real systemd isolation smoke tests remain
local opt-in checks.

See [fixture provenance](../internal/domain/testdata/README.md) for the offline
capture paths. Go records are transport contracts only: decoding them does not
validate signatures, determine eligibility, or authorize work.
