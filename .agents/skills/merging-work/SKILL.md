---
name: merging-work
description: Applies final merge gates to verified pull requests, merges eligible work, records final evidence, marks issues DONE, and unlocks dependent tasks. Use only after independent review and CI.
---

# Merging Work

Never merge because an agent says "done".

Required gates: correct issue/PR linkage, acceptance criteria satisfied, review evidence accepted, security review accepted when required, required CI green, no unresolved blockers, correct target branch, dependency policy satisfied.

Prefer repository-configured merge queue when available.

After merge: record merged PR/commit, set DONE, close issue when appropriate, re-evaluate blocked dependents, set newly unblocked tasks READY, and clean disposable worktree only after evidence is durable.
