# State Machine

```text
BACKLOG
   -> PLANNING
   -> READY
   -> CLAIMED
   -> IN_PROGRESS
   -> IMPLEMENTED
   -> REVIEWING
        |-> CHANGES_REQUESTED -> IN_PROGRESS
        |-> VERIFIED
   -> MERGE_READY
   -> MERGING
   -> DONE
```

Side states:

```text
BLOCKED
RETRY_PENDING
WAITING_HUMAN
WAITING_PROVIDER
FAILED
```

Coordinator owns durable Project transitions. Implementers/reviewers produce evidence; they do not decide durable project state.

## Recovery examples

- `IN_PROGRESS` + inactive worker + no valid result -> `RETRY_PENDING` or `WAITING_PROVIDER` depending on cause.
- `IMPLEMENTED` + local commit + no PR -> coordinator validates, pushes, opens PR and starts review.
- `REVIEWING` + reviewer disappeared + no result -> retry review, not implementation.
- `VERIFIED` -> re-check CI before merge.
- `MERGE_READY` -> re-check every gate; do not trust stale status.
- `BLOCKED` -> re-query native dependencies.
