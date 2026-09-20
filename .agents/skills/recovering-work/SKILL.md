---
name: recovering-work
description: Reconstructs and repairs harness execution state after coordinator/worker/provider crashes, machine restarts, stale statuses, missing sessions, or partial publication. Use at the start of every coordinator cycle and whenever durable state disagrees with live execution.
---

# Recovering Work

Conversation history is optional.

Inspect GitHub Project active items, issue dependencies, linked branches, open PRs, CI/review evidence, transient systemd units, local job/results metadata, and worktrees/commits.

Reconcile:
- IN_PROGRESS + active worker -> leave active
- IN_PROGRESS + inactive worker + valid result/commit -> continue publication
- IN_PROGRESS + inactive worker + no valid result -> retry by policy
- IMPLEMENTED + no PR -> publish/open PR
- REVIEWING + valid result -> process it
- REVIEWING + dead reviewer/no result -> retry review
- VERIFIED -> recheck CI
- MERGE_READY -> recheck every gate
- BLOCKED -> re-query dependencies

Never infer success from stale status.
