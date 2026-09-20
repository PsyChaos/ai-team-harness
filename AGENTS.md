# AI Team Harness Instructions

This repository uses `.ai-team/` as its autonomous development operating contract.

1. Read `.ai-team/config/harness.yaml`.
2. Read the skill assigned to your role.
3. Read applicable policies.
4. Work on exactly one assigned GitHub issue.
5. Respect the assigned worktree/branch.
6. Never expand scope silently.
7. Produce validation evidence.
8. Never merge your own work.
9. GitHub is durable work state.
10. `.ai-team/runtime/` is disposable local execution state.

## Decision engine

The coordinator may use `.ai-team/bin/decide`.

Jev is optional. It supplies fuzzy routing judgments only.

Never use Jev to replace exact checks for CI, dependency state, worker liveness,
branch/PR existence or merge eligibility.

Policy always overrides a routing result.
