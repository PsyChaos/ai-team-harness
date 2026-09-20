# Canonical Harness Events

Provider hook names differ. Harness workflow uses these conceptual events:

```text
TASK_READY
TASK_ASSIGNED
WORKER_STARTED
WORKER_FINISHED
WORKER_FAILED
PR_CREATED
REVIEW_STARTED
REVIEW_APPROVED
REVIEW_REJECTED
CI_PASSED
CI_FAILED
MERGE_REQUESTED
MERGED
TASK_COMPLETED
```

Provider hooks are guardrails/telemetry around execution, not the durable orchestration engine. Durable transitions remain coordinator-owned in GitHub.
