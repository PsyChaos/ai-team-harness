# Temporary Python observer bridge

`internal/observer/pybridge` and `cmd/pybridge` are **temporary, migration-only**
components. Remove them once native Go execution is active. They observe the
Python broker and GitHub; they never schedule work, execute actions, update
GitHub, verify broker signatures, or write Python state. GitHub remains durable
authority. An expired snapshot is allowed as historical observation; its original
creation and expiry timestamps remain visible. Proposal counts are not completed
actions or live workers.

The adapter uses the existing `domain.Snapshot`, `ghgateway.Reader`, and
`journal.Event` contracts. It reads regular snapshot files through `os.Open`,
bounds JSON input to 16 MiB, and rejects invalid envelopes, trailing JSON, failed
GitHub reads, repository mismatches and sequence overflow without returning
partial events. Unknown input fields are tolerated but never copied wholesale.

## Observation payloads

Events use the established journal envelope (`id`, `type`, `data`):

- `pybridge.broker.observed`: repository, broker creation/expiry timestamps, and
  proposed action count. Signed action IDs, signatures, nonces and action payloads
  are excluded.
- `pybridge.task.observed`: repository, issue number, issue state, Harness Status,
  Provider, Agent Role, Retry Count, and dependency repository/number/state with
  the explicit completeness flag. Non-Issue Project items are skipped. Bodies,
  arbitrary field values, URLs, local paths and opaque metadata are excluded.

GitHub fields describe the current read, independently of the older broker
snapshot. They are not an atomic cross-source view. The source may change between
reads. No status implies a live worker, lease or dispatch authorization. Consumers
must render all textual values as text, never HTML. This is an allowlisted
projection, not a general secret detector for user-entered Project fields.

`Observe(ctx, snapshotPath, reader, cursor)` returns consecutive events starting
at `cursor + 1`. The caller must exclusively own a dedicated journal with no
pending records and pass its persisted cursor. Repeated reads produce new
observations, not lifecycle transitions; there is no polling or scheduling loop.
Append errors remain journal errors, and a multi-event append is not atomic.

## One-shot local use

For live GitHub reads, install/authenticate `gh` with read access and run:

```sh
go run ./cmd/pybridge -snapshot /path/to/current-broker-snapshot.json \
  -repo owner/repository -owner project-owner -project 1
```

Only fixed owner GET and the gateway's compiled-in GraphQL queries are used.
There are no user-supplied queries, mutation commands, broker imports or broker
subprocesses. GitHub failures stop observation rather than using stale fallback
state. The command has a 60-second read timeout.

For offline replay, supply an array of normalized `domain.ProjectItem` objects
(the Python broker's `project_items` output, not flattened `gh project` output):

```sh
python3 -c 'import json; print(json.dumps([json.load(open("internal/domain/testdata/project-item.json"))]))' > /tmp/pybridge-items.json
go run ./cmd/pybridge -snapshot internal/domain/testdata/snapshot.json \
  -github-items /tmp/pybridge-items.json
```

The command creates a **new private temporary directory** and writes only its Go
journal there; it logs the path to stderr and emits the journal snapshot to
stdout. Input paths can never be selected as output destinations. The journal
is deliberately retained for inspection; remove its temporary directory when
finished. Observation is read-only with respect to Python and GitHub, not with
respect to this separate journal. The dashboard can now follow this journal using `cmd/harness -journal PATH`
(see [live dashboard](validation/issue-13-dashboard.md));
a consumer can use these events through the existing journal HTTP handler.

## Validation and provenance

```sh
go test ./internal/observer/pybridge/...
go build ./...
go vet ./...
go test -race ./...
```

Tests reuse the actual Python-produced `snapshot.json` and `project-item.json`
under `internal/domain/testdata`; see that directory's README and `capture.py`
for reproducible provenance. These are controlled offline captures, not live
production exports. Tests check filtering, source immutability, schema and
journal persistence/restart, dependency completeness, and failure boundaries.
They make no GitHub calls or writes to broker state.

A production-snapshot manual check requires a real current snapshot and readable
GitHub Project. Hash the input before and after running the command, inspect the
printed journal for matching repository/timestamps and task states, and confirm
the source hash is unchanged. Do not run broker snapshot generation as part of
this observer: that would cross the read-only boundary.
