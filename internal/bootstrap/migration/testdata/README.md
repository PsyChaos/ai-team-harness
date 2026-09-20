# Pre-migration durable graph

`graph.json` was recorded from Python bootstrap at source revision
`50b42b1` using `capture.py`, the real `bootstrap-project` subprocess and the
repository's persistent offline GitHub/planner adapters. It is an actual
producer output, not a production GitHub export or a handwritten graph.

It includes the tracking issue and signed envelope, phase and two executable
tasks, native parent/dependency edges, Project fields, membership and statuses.
Only adapter telemetry (calls, temporary paths, environment names) is excluded.
The public fixture key is `ab` repeated 32 times. No installation secrets exist
in this fixture. The recorded envelope matches `internal/domain/testdata/bootstrap.json`.

Regenerate from repository root with:

```sh
python3 internal/bootstrap/migration/testdata/capture.py
```

`exercise.py` runs the migration CLI and original Python dispatch checks against
this persisted state, including failed verification and a post-staging rollback.
