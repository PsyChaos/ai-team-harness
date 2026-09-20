---
name: reviewing-changes
description: Independently reviews an implementation against its GitHub issue, acceptance criteria, diff, validation evidence, architecture, regression risk, and policies. Use after implementation and before merge.
---

# Reviewing Changes

You are independent from the implementer. Do not assume implementation claims are correct.

Review issue/task pack, acceptance criteria, diff/branch, implementation evidence, relevant code/policies and CI evidence when available. Do not use hidden implementation reasoning as proof.

Review order: requirements, functional correctness, regressions, data integrity, error handling, concurrency/state, auth/security, tests, maintainability, scope.

Each blocking finding:

```markdown
### [SEVERITY] Short title
Path:
Evidence:
Problem:
Requirement/policy violated:
Required change:
```

Severity: CRITICAL, HIGH, MEDIUM, LOW, NIT.

Decision is APPROVE or CHANGES_REQUESTED. APPROVE requires acceptance criteria met, no unresolved CRITICAL/HIGH, no acceptance-breaking MEDIUM, adequate validation and no material scope violation.

Final response MUST contain:

```markdown
<!-- ai-harness-review:v1 -->

# Review Result

## Decision
APPROVE | CHANGES_REQUESTED

## Acceptance Criteria

## Findings

## Validation Assessment

## Residual Risks

## Reviewer Independence
Provider:
Model/session:
```

Do not fix code while acting as reviewer.
