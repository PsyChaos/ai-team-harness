# Deterministic Coordinator Broker

`coordinator-cycle` executes `coordinator-broker run`. It never starts Claude,
Codex, Gemini, Jev, or another model as coordinator. Models remain isolated
implementers/reviewers and cannot mutate GitHub or load coordinator secrets.

## Security boundary

The broker reads GitHub/Git/systemd state with argv-only subprocess calls
(`shell=False`). It creates an HMAC-signed, five-minute snapshot containing only
allowlisted actions. Every opaque action ID binds the repository, issue, expected
project state, provider/profile, snapshot nonce and content fingerprint. Apply
rejects modified, expired, unknown, duplicate or stale actions and re-reads state
immediately before mutation.

GitHub issue bodies are untrusted data. They can appear only inside a delimited
Task Pack and are never interpreted as commands, paths, statuses or GitHub
operations.

Codex implementers can write the assigned clone, including its independent
`.git` directory so they can create local commits. Reviewers retain read-only
access to both. The disposable Codex home remains denied to model tools, and
the outer bubblewrap boundary protects the coordinator checkout and secrets.

## Implemented lifecycle

### Provider quota recovery

A failed provider process with a recognized quota message on stderr enters
`WAITING_PROVIDER`, preserving the clone and assigned task. The next cycle selects
an enabled, installed provider from `HARNESS_IMPLEMENTER_ORDER` or
`HARNESS_REVIEWER_ORDER` that is outside its quota cooldown. Reviewers must still
use a different provider from the implementer. If none is eligible, the task waits
and is reconsidered automatically after cooldown; no human approval is needed.
`HARNESS_PROVIDER_COOLDOWN_SECONDS` defaults to 900 (allowed range 30–86400).

Quota continuation does not consume code-correction or reviewer process retries.
Recovery binds the issue, branch, task pack, failed job/result and clone HEAD.
Interrupted launches retain their recovery entitlement. Existing sibling review
decisions, including rejections, are preserved. CI, review and merge gates remain
mandatory. Unknown process errors continue through the ordinary bounded retry
path; matching a quota phrase in model output alone does not trigger fallback.

### Task transitions

The broker implements one restart-safe transition per item per cycle:

1. require a signed, complete bootstrap executable leaf;
2. require READY + Implementer + known enabled provider;
3. require every native blocked-by issue to be CLOSED;
4. enforce global worker capacity;
5. create a canonical isolated `.worktrees/issue-N` clone (no shared Git metadata)
   and deterministic Task Pack;
6. transition READY → CLAIMED → IN_PROGRESS around duplicate-safe systemd spawn;
7. reconcile inactive workers using a strict structured result plus clean-clone,
   commit/base/diff/path validation against the exact GitHub base SHA bound before
   dispatch (worker-controlled remote refs are never trusted);
8. publish a non-divergent branch, create/recover one PR, and dispatch independent
   standard and (for HIGH/CRITICAL risk) security review;
9. convert role/provider/head-bound results into broker-HMAC-signed PR evidence;
10. re-read the PR head, issue link, reviews, mergeability, and CI immediately
    before both MERGE_READY and merge;
11. merge with `--match-head-commit` (never `--admin`), record durable evidence,
    close the issue, mark DONE, clean disposable state, and unlock dependents only
    when every native blocker is closed.

Empty CI is fail-closed unless `HARNESS_ALLOW_EMPTY_CI=1` is deliberately set.
Remote divergence and exhausted retries move work to `WAITING_HUMAN`; the removed
LLM coordinator is never a fallback.

The installed `Harness CI` workflow runs offline Python tests, secret/provider
boundary checks, and shell syntax checks on pushes and pull requests. Real
systemd isolation and authenticated Codex smoke tests run locally; the latter
checks local commits as well as credential-read denial.

## Diagnostic interface

```bash
.ai-team/bin/coordinator-broker snapshot --output /tmp/snapshot.json
.ai-team/bin/coordinator-broker apply --snapshot /tmp/snapshot.json --decisions /tmp/decisions.json
```

The decisions file schema is exactly:

```json
{"selected_action_ids":["a_<opaque-hmac>"]}
```

Normal operation uses `.ai-team/bin/coordinator-cycle`.

## Existing v1 roots and future key rotation

`migrate-secrets` moves the existing v1 bootstrap key byte-for-byte; it never
re-signs roots and never silently replaces a missing key in an existing store.
This keeps all current v1 graph signatures valid. Back up the external store.

Automatic rotation is deliberately unsupported in this increment. A future v2
rotation command must be resumable: retain active and previous key IDs, verify
every root/binding/content/native edge with the old key, re-sign each root as v2
with the active key, accept both IDs during interruption, and remove the previous
key only after every live root verifies as v2. Until that command exists, restore
the original key instead of rotating it.
