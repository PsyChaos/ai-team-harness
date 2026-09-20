# GitHub Project Contract

## Native relationships

Use GitHub native:

- parent / sub-issue
- blocked-by
- blocking

Do not encode dependencies only in prose.

## Custom fields

`setup-github-project` creates missing fields.

### Harness Status

BACKLOG, PLANNING, READY, CLAIMED, IN_PROGRESS, IMPLEMENTED, REVIEWING, CHANGES_REQUESTED, VERIFIED, MERGE_READY, MERGING, DONE, BLOCKED, RETRY_PENDING, WAITING_HUMAN, WAITING_PROVIDER, FAILED.

### Priority

P0, P1, P2, P3.

### Risk

LOW, MEDIUM, HIGH, CRITICAL.

### Work Type

Architecture, Backend, Frontend, Mobile, Database, DevOps, Test, Security, Documentation, Refactor, Bug.

### Provider

Claude, OpenAI, Gemini, Local, Other.

### Agent Role

Planner, Implementer, Reviewer, Security Reviewer, Merge Manager.

### Model

Text.

### Retry Count

Number.

### Evidence

Short text; detailed evidence belongs in issue/PR comments.

## Required executable task format

```markdown
## Goal

## Context

## Scope

### In scope

### Out of scope

## Acceptance Criteria

- [ ]

## Dependencies

## Constraints

## Validation

## Evidence Required

## Risk
```

## READY definition

An issue may be READY only when its parent permits execution, blocked-by dependencies are complete, scope/goal are clear, acceptance criteria exist, required product decisions are resolved, and no conflicting exclusive task owns the same critical area.

## DONE definition

For code-changing tasks: implementation committed, branch published, PR created, acceptance criteria verified, independent review evidence accepted, configured CI passed, PR merged, and final evidence stored.
