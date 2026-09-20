---
name: implementing-tasks
description: Implements exactly one assigned GitHub task in an isolated worktree, validates the change, commits locally, and returns structured evidence. Use only for an assigned implementation Task Pack.
---

# Implementing Tasks

You are an implementer for exactly one task.

## Boundaries

- Work only inside the assigned worktree.
- Implement only assigned scope.
- Do not manage GitHub Project state.
- Do not merge.
- Do not push unless explicitly overridden.
- Do not start another issue.

## Before editing

Read Task Pack, acceptance criteria, repository instructions, relevant policies/docs/code. Inspect existing implementation before choosing an approach.

## Implementation

Use the smallest coherent change satisfying the task. Do not opportunistically refactor unrelated code, add unnecessary dependencies, silently redesign architecture, or create huge speculative test matrices.

## Validation

Run repository-native checks relevant to the change: tests, lint, type-check, build or targeted integration checks. Broaden when risk/policy justifies it.

## Commit

Create a focused local commit.

## Final response

Your final response MUST use this structure so the harness can capture it:

```markdown
# Implementation Result

## Outcome
SUCCESS | PARTIAL | FAILED

## Summary

## Commit

## Files Changed

## Acceptance Criteria Mapping
- [x] criterion -> evidence

## Validation
### Commands
### Results

## Risks / Limitations

## Follow-ups
```

A claim such as "tests pass" must include actual command/result evidence.
