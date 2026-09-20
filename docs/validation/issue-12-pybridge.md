# Issue 12 local validation

## Review correction and provenance

The required real-snapshot manual observation has now been performed using the
broker-supplied coordinator capture and corresponding normalized GitHub read
state. This supersedes the earlier fixture-only report and its unavailable-input
caveat. No production code changed in this correction.

- Executed: 2026-09-20, completed by 22:23:22 UTC (2026-09-21 locally).
- Source commit tested: `84f78e89689fa20e3ff150487585ed64d2c89ec6`.
- Environment: Go `go1.27.0 linux/amd64`; `GOCACHE=/tmp/issue-12-go-cache`
  and `GOPROXY=off` for the commands below.
- Input provenance: the registered broker validation context in the issue-12
  retry Task Pack supplied these real coordinator inputs. The coordinator
  attested registration/signature/repository/issue/HEAD binding. That attestation
  supplies provenance, not a passing test result; the checks below were executed
  locally against the unchanged implementation at the source commit above.
- The interrupted correction had already saved the two JSON inputs; they are
  preserved here as [snapshot.json](issue-12-capture/snapshot.json) and
  [github-items.json](issue-12-capture/github-items.json). They contain the
  supplied snapshot and all 28 normalized Project items, with JSON formatting.
  No issue bodies, credentials, or broker signing keys are included.

The snapshot was current when captured by the broker (`created_at=1789937659`,
`expires_at=1789937959`). This is an offline replay of that real capture, not a
fresh live GitHub query or a claim that the snapshot remained unexpired at replay
time. The observer intentionally accepts historical snapshots without applying
or authenticating their actions. No broker state was regenerated.

## Reproducible manual run

From the repository root:

```sh
GOCACHE=/tmp/issue-12-go-cache GOPROXY=off \
  python3 docs/validation/issue-12-capture/replay.py
```

The [replay script](issue-12-capture/replay.py) copies the inputs into temporary
files with mode `0444`, then executes:

```sh
go run ./cmd/pybridge -snapshot <temporary-directory>/snapshot.json \
  -github-items <temporary-directory>/github-items.json
```

It compares every projected field against the captured input, verifies consecutive
journal event IDs and cursor, reads the separately persisted journal, and checks
that both temporary inputs and repository captures retain exactly their original
bytes. A failing assertion or command exits nonzero. The offline reader does not
invoke `gh`; neither the script nor adapter invokes a Python broker process.
The adapter's source reads use `os.Open`; writes are confined to its new private
Go journal directory. Hash equality demonstrates unchanged input contents, while
inspection of this execution path establishes the read-only source access.

Actual result: exit 0, `PASS`. The [recorded result](issue-12-capture/result.json)
and [persisted journal](issue-12-capture/journal.json) are committed for review;
review does not depend on continued access to `/tmp`.

| Input | SHA-256 before and after (identical) |
| --- | --- |
| `snapshot.json` | `ff8384fe0c20b20dd2d20dca0eab6da2597d421440dbcc1f942e8f0679bc806f` |
| `github-items.json` | `aee9606d2471014a615f7abe05ab7886d2d943b74b497e17476c822abad40b79` |

The journal has version 1, cursor 29, no pending entries, and consecutive IDs
1–29. Event 1 is `pybridge.broker.observed`, repository
`PsyChaos/ai-team-harness`, original timestamps above, zero proposed actions.
Events 2–29 are `pybridge.task.observed` for issues 1–28. All task states, statuses,
providers, roles, retry counts, dependency states and completeness flags match
the capture. For example, issue 12 is OPEN/WAITING_HUMAN with retry count `2`,
and issue 9 is OPEN/BLOCKED with complete dependencies 8/CLOSED and 5/OPEN.
Unknown dependency completeness remains false. The stdout snapshot matches the
persisted journal; signature, nonce and opaque Project item IDs are absent from
the projected events.

## Local checks

| Command | Result |
| --- | --- |
| `go test -count=1 ./internal/observer/pybridge/...` | PASS (`ok`, 0.008s), recorded Python fixtures |
| `python3 docs/validation/issue-12-capture/replay.py` | PASS, real captured inputs, all 29 persisted events checked |
| `go build ./...` | PASS, exit 0 |
| `go vet ./...` | PASS, exit 0 |
| `go test -race ./...` | PASS, all Go packages |
| `git diff --check` | PASS, exit 0 |

The temporary/migration-only lifecycle remains documented in the package,
command, and [bridge documentation](../python-observer-bridge.md). Existing
recorded-fixture tests cover nonempty action proposals, which this real capture
(with zero actions) cannot exercise.

## External handoff

Hosted CI for the correction commit remains pending. The coordinator must publish
and obtain CI and independent review for that commit. Prior hosted-CI claims are
not treated as evidence for this new revision. This validation grants no review
or merge approval; no push or GitHub query was performed.
