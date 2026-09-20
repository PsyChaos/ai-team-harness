# Review Policy

Review in this order:

1. acceptance criteria
2. functional correctness
3. regression risk
4. data integrity
5. error handling
6. concurrency/state
7. authentication/authorization/security
8. test adequacy
9. maintainability
10. scope discipline

Blocking findings require severity, path, concrete problem, evidence/reproduction/code path, violated requirement/policy and required change.

Severities: CRITICAL, HIGH, MEDIUM, LOW, NIT.

Default merge blockers are CRITICAL/HIGH. MEDIUM also blocks when it violates acceptance criteria or creates a material defect.
