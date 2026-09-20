# GitHub reader parity

`responses.json` is an offline recording of GitHub-shaped API responses for an
invented `acme/widget` graph, not a live GitHub export. It records the owner lookup,
two Project pages and one dependency batch. The graph covers DONE, READY, CLAIMED,
IN_PROGRESS, RETRY_PENDING and BLOCKED tasks; absent/zero/nonzero retries; closed,
open, empty and truncated dependency lists.

`python-state.json` is actual output captured by executing the repository's
current `.ai-team/coordinator/broker.py::project_items` and `field_value` on those
responses. It is not a hand-written expected Go result. The capture replaces only
`json_run`, verifies the request sequence/variables, clears the environment, and
forbids subprocess execution. No credentials or live API are needed.

Run from the repository root:

```sh
go test ./internal/ghgateway/...
python3 internal/ghgateway/testdata/parity.py
```

The script executes the current Python broker, checks the committed capture, then
runs the native Go parity test and compares the decoded JSON field-by-field
(including array order and null versus empty). A mismatch emits a unified diff
and exits nonzero. Refresh intentional Python contract changes with:

```sh
python3 internal/ghgateway/testdata/parity.py --capture
```

## Boundaries inherited from Python

Only BLOCKED items get dependency hydration. Other issues retain an empty list
with `complete: false`; a truncated BLOCKED list also retains `complete: false`.
This is not evidence that those tasks are runnable. Retry counts are Project
field strings, with integral numbers normalized (2.0 becomes "2"); missing fields
remain empty, rather than silently assigning a retry budget.

The existing broker does **not** query issue labels or GitHub leases. The parity
projection explicitly records both as null/unavailable. CLAIMED and IN_PROGRESS
are durable statuses, not proof of a current worker lease. Worker liveness comes
from local systemd/job reconciliation elsewhere in Python, outside this GitHub
reader. Adding a lease protocol or treating labels as lifecycle authority would
change the broker contract and belongs to a separate task.

## Reader interface

`ghgateway.Gateway` implements the existing `Reader` interface. Supply a read-only
`Source` implementing owner lookup and GraphQL response retrieval, then call
`ProjectItems(ctx)` and optionally `Reconstruct(items)`. Source implementations
own transport/authentication and must report failures. This task supplies no live
transport or GitHub write surface; test sources replay recorded responses.

The reader preserves the twenty-page Project bound, rejects archived items,
foreign repository issues and truncated fields, and hydrates dependencies in
batches of one hundred. Dependency identity, shape and state are validated.
GraphQL errors fail the Go read even when partial data is present (a deliberate
stricter failure boundary than the Python dictionary reader). Non-Issue Project
items retain their content type but use zero values in the existing Go Issue
struct; consumers must inspect Type before treating them as tasks.
