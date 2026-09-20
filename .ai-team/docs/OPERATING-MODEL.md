# Operating Model

## Scheduling order

Prefer finishing existing work before starting more:

1. reviews/merges near completion
2. P0
3. critical-path unblockers
4. HIGH/CRITICAL risk work
5. short independent work
6. ordinary backlog

## Parallelism

Defaults: 4 implementers, 2 reviewers, 1 security reviewer. Lower these for small repositories, weak hardware, migration-heavy work, expensive providers or shared test databases.

## File overlap

Do not run strongly overlapping implementation tasks in parallel. Serialize tasks likely to edit the same central module/migration chain unless independence is explicit.

## Retry

Attempt 1 uses the selected provider/session. Attempt 2 is a fresh session and
preferably a different provider/model. Each implementation retry pack repeats the
broker-fetched original issue scope, acceptance/validation text, closed
dependencies, and workspace identity; prior failure or rejecting-review evidence
is included only as a bounded supplement. Workers never need GitHub credentials
or network access to recover scope. After the configured retry limit, use FAILED
or WAITING_HUMAN based on cause. Provider outage is WAITING_PROVIDER, not FAILED.

## Review

Reviewer must be a different session. Prefer a different provider/model. HIGH/CRITICAL tasks may require both normal and security review evidence.

## Context

Task Packs should include relevant durable context only: issue, parent goal, acceptance criteria, dependencies, constraints, relevant docs/files, and prior failure/reviewer evidence when applicable. Do not dump whole chats or unrelated project history.
