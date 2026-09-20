# Recovery

## Operator recovery and visible progress

`status` reads `HARNESS_COORDINATOR_UNIT` (default `ai-harness-coordinator`).
Set it to the actual repository-specific timer basename. Harness transitions
also synchronize GitHub's standard Status field: active implementation, review,
and publication stages map to In Progress; DONE maps to Done. Detailed states
remain in Harness Status. Issue comments record transitions and available
evidence; a timer tick overlapping an existing cycle is skipped successfully.

After fixing the cause of a WAITING_HUMAN implementation, use:

```bash
.ai-team/bin/coordinator-broker resume --issue 3 --reason 'Describe the corrected cause'
.ai-team/bin/coordinator-broker sync-status
```

Resume preserves the clone, audits the previous attempt count, and grants one
additional implementation attempt. It never approves acceptance or merges code.
Do not repeatedly resume an unchanged failure.

Complete local implementations may report VALIDATION_PENDING, keeping only
`- [ ] [external:ci] ...` or `- [ ] [external:browser] ...` unchecked. Publication
precedes hosted CI. Ordinary incomplete implementation still fails the handoff.
Independent review and CI remain required; a deferred browser check additionally
requires a signed successful external report bound to the exact commit.

An operator who has actually performed external browser validation can supply
`resume --validation-report /absolute/report.md`. The report must name the full
current HEAD and include a `Decision: PASS` or `Decision: APPROVE` line. The broker
signs this evidence and supplies it to the retry worker and reviewer; a changed
HEAD invalidates it. Never use this option to waive missing validation.

After validating a new commit without resuming implementation, use
`coordinator-broker record-browser-validation --issue N --report /absolute/report.md`.
The same exact-commit and signature requirements apply.

A VERIFIED PR conflicting with a newer default branch receives one broker-bound
upstream integration attempt. The broker fetches the trusted default branch and
records both parent commits before preparing a local merge. The assigned worker
resolves conflicts and commits locally; publication remains a fast-forward push.
Interrupted preparation can replay the pending transition. CI and independent
review run again, and prior browser evidence must be renewed for the new HEAD.
A second upstream conflict requires operator inspection.

Reviewer process failures are retried separately, within HARNESS_MAX_RETRIES;
they do not trigger implementation changes. Before launching Claude, an expired
file-backed OAuth credential is refreshed by the trusted CLI outside the
read-only worker mount, with tools, hooks, MCP, and repository settings disabled.

The deterministic broker reconstructs each action from GitHub, Git, systemd, and
strict local evidence. Snapshots expire after five minutes and apply rechecks the
item fingerprint plus bound PR/head/result digests. A cycle advances at most one
transition per item.

READY dispatch is restart-safe before spawn: proven pre-spawn failures clean the
new isolated clone/branch/pack and restore READY. An ambiguous systemd spawn
leaves CLAIMED to avoid duplicate execution. An inactive valid worker advances to
IMPLEMENTED; a structured failure enters bounded retry. Existing remote branches
and PRs are re-read for idempotency. Lost PR-create and merge responses recover
only when fresh GitHub state proves the exact branch/head outcome. Divergence
fails to WAITING_HUMAN without force-push.

Implementation retries never depend on worker-side GitHub access. Every retry
action signs a fingerprint of a freshly fetched, normalized open issue scope.
Immediately before spawning, the broker fetches it again and fails closed if the
title, body, URL, dependency closure, acceptance criteria, or validation content
changed. The v2 retry pack retains the complete original issue body and workspace
identity plus bounded prior failure/review evidence. Scope-less retry packs
created by the affected earlier release receive one broker-detected retry credit
during migration; the v2 marker prevents that credit from being applied twice.

## Bootstrap credentials and signed state

Job/result/log caches are disposable. `runtime.env` is non-secret configuration.
Secrets live outside the repository at `HARNESS_SECRETS_FILE`; its directory is
0700 and file is 0600. Back up that external store. In particular,
`HARNESS_BOOTSTRAP_HMAC_KEY` authenticates the durable GitHub plan and issue bindings.
Never delete, regenerate or rotate it to bypass a failed signature check. Restore
the original key from backup when missing. A configured key makes implementer
dispatch signed-graph-only; ordinary unsigned issues cannot be used as a fallback.

Resume interrupted publication by rerunning `bootstrap-project` with the same
brief. It verifies the signed root, recovers canonical issue bindings and repairs
missing native edges before finalizing. Do not unlock a partially published graph
by editing Project status or adding a completion marker manually.

## Startup

1. load local runtime variables
2. read harness configuration/policies
3. query active Project items
4. query open PRs
5. inspect issue dependencies
6. inspect transient systemd units
7. inspect local job/results metadata
8. reconcile inconsistencies
9. continue scheduling

## Never trust status alone

`IN_PROGRESS` does not prove a worker exists. `IMPLEMENTED` does not prove a commit exists. `VERIFIED` does not prove review evidence exists. `MERGE_READY` does not prove CI is still green.

## Machine reboot

GitHub state survives; transient workers do not. After boot, the timer resumes, stale active states are reconciled, and incomplete work is retried according to policy.

## Failed isolated clone

Before deleting/replacing it, preserve useful diff/commit/test/failure evidence. Cleanup occurs only after durable evidence exists.
