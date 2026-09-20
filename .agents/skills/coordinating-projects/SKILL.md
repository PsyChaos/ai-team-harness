---
name: coordinating-projects
description: Documents the deterministic GitHub coordinator broker. Provider models are workers only and never receive coordinator authority.
---

# Coordinating Projects

## Security override (v1.2)

The coordinator is `.ai-team/coordinator/broker.py`, not an LLM session. Run only
`.ai-team/bin/coordinator-cycle`. Never give Claude, Codex, Gemini, Jev, issue
text, PR text, or any other model/free-form input `gh`, `git`, `systemctl`, shell,
secret, project-field, publication, or merge authority. The remaining lifecycle
notes describe policy for deterministic broker increments; they are not model
tool instructions.

## Absolute boundary

Do not implement product code.

Only the broker may inspect or mutate Git/GitHub/systemd state, create Task Packs,
manage worktrees, spawn agents, publish branches, update project fields, or merge.

Do not use product-code editing as a substitute for dispatching an implementer.

## Start every cycle fresh

Read:

- `.ai-team/config/harness.yaml`
- `.ai-team/policies/GIT.md`
- `.ai-team/policies/REVIEW.md`
- `.ai-team/docs/STATE-MACHINE.md`
- `.ai-team/docs/RECOVERY.md`
- `.ai-team/docs/COORDINATOR-COMMANDS.md`
- `.ai-team/docs/DECISION-ENGINE.md`

Load non-secret `.ai-team/runtime/runtime.env`; privileged broker/bootstrap entrypoints
then load the external 0700/0600 secret store. Reconstruct state deterministically.
Do not rely on previous conversation history.

## Decision engine

The harness may use `rules` or optional TypeSafe Jev.

Never ask Jev questions that code/GitHub/systemd can answer exactly.

Never delegate these to Jev:

- CI passed?
- dependency complete?
- worker alive?
- branch/PR exists?
- PR merged?
- merge gate satisfied?

Use Jev only for fuzzy judgment such as:

- work classification
- provider selection
- model capability profile
- independent review level
- retry vs provider switch vs replan/split
- next action among actions already proven valid by deterministic state

Call only:

```bash
.ai-team/bin/decide task --state-file FILE
.ai-team/bin/decide retry --state-file FILE
.ai-team/bin/decide action --state-file FILE
```

Do not call the TypeSafe API directly.

A decision with `source=rules-fallback` is valid fallback behavior, not an error.

Policy/capacity/security constraints always override a model decision.

## Cycle order

1. Reconcile active worker/reviewer jobs.
2. Process completed implementation results.
3. Publish valid branches and create/update PRs.
4. Dispatch/consume independent reviews.
5. Re-check CI and merge gates.
6. Merge eligible PRs.
7. Mark DONE and unlock dependents.
8. Re-evaluate BLOCKED/WAITING_PROVIDER work.
9. Plan PLANNING work.
10. Route and dispatch READY items within capacity.
11. Exit cleanly.

Prefer finishing existing work over starting more work.

## Task routing

For a READY task, build a compact JSON state containing:

- issue number/title/body or concise relevant content
- risk
- work_type if already known
- available_providers
- relevant language/framework hints
- critical-file/concurrency/security context

Run:

```bash
.ai-team/bin/decide task --state-file STATE.json
```

Use the returned provider/model profile/review mode if allowed by policy and capacity.
Record significant routing evidence when useful.

`model_profile` means capability class (`fast`, `balanced`, `strong`), not a
vendor model name. Map it to configured provider models/policies.

## Retry routing

When an attempt fails, build state with:

- failure_kind
- failure evidence
- attempts
- previous_provider
- available_providers
- task/risk context

Run:

```bash
.ai-team/bin/decide retry --state-file STATE.json
```

Then enforce retry limits and human-gate policy deterministically.

## Action routing

Only when multiple actions are already valid, provide exactly those actions:

```json
{
  "allowed_actions": ["retry", "replan", "split_task"],
  "context": "..."
}
```

Jev may rank/select among them. It must never make an otherwise-invalid action valid.

## Planning

`bootstrap-project --brief FILE` creates a durable root and a validated issue
graph. The root's first line is `<!-- ai-harness-bootstrap:v1:KEY -->`; its body
contains the full plan. Root and grouping issues have
`<!-- ai-harness-node:container -->` and must NEVER be dispatched to implementers.
Executable nodes have `<!-- ai-harness-node:executable -->` and a Bootstrap root URL.
Before unlocking or dispatching ANY bootstrap descendant, fetch that root and
require its body to end with `<!-- ai-harness-bootstrap:complete -->` on its own
line. Until then, the entire graph is incomplete, even if a Project item says
READY. Resume publication by rerunning bootstrap-project with the same brief;
do not independently replan an incomplete bootstrap root or unlock its children.
After completion, check native dependencies before dispatch. Close grouping/root
issues only when every descendant has completed and passed normal review/CI gates.
The root binds each node to an issue number, URL and canonical title/body digest.
Do not replace node identity by matching markers alone. `spawn-agent` verifies the
binding, canonical content, publisher and native edges; modified/spoofed tasks
must be reconciled explicitly before dispatch, never bypass this preflight.
When HARNESS_BOOTSTRAP_HMAC_KEY is configured, all implementation tasks must belong
to a signed bootstrap graph. Do not create ordinary unsigned split/replan/manual
tasks and dispatch them; signed bootstrap publication is required. Never delete
the signing key or bypass the preflight to execute unsigned work.
Bootstrap records initial Provider/Model routing; re-evaluate at dispatch using
current provider availability and capacity. A `profile:... (CLI default)` Model
value is a capability profile, not a literal CLI model name.

When an item is PLANNING, use planning rules and create real GitHub issues with
native parent/dependency relationships.

## Dispatch

Before implementation:

- status READY
- dependencies satisfied
- acceptance criteria present
- scope bounded
- no conflicting exclusive task active
- worker slot available

Create an isolated worktree and Task Pack, then use `spawn-agent`, passing the selected model profile:

```bash
.ai-team/bin/spawn-agent implementer PROVIDER ISSUE WORKTREE TASKPACK MODEL_PROFILE
```

Use `fast`, `balanced`, or `strong`. For independent reviews, normally use
`balanced`; use `strong` for security/high-risk/dual review.

## Completion and review

Worker output is evidence, not authority.

Verify commit/result/scope, publish branch/PR, then use an independent reviewer.
Prefer another provider/model. Use security/dual review when routing/policy requires it.

## Merge

Merge only after deterministic gates pass. Jev does not decide merge eligibility.

## Durable action rule

After every meaningful transition, GitHub must reflect durable state before the cycle exits.
