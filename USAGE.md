# Usage

## Secure coordinator

First migrate legacy secrets, then run a cycle:

```bash
.ai-team/bin/migrate-secrets
.ai-team/bin/harness-doctor
.ai-team/bin/coordinator-cycle
```

The cycle uses no coordinator model. It deterministically advances eligible work
through dispatch, reconciliation, publication, independent review, CI/head gates,
merge, DONE, cleanup, and native dependency unlock. Each cycle applies at most
one signed, freshly validated transition per item. Empty CI fails closed unless
`HARNESS_ALLOW_EMPTY_CI=1` is explicitly configured.

## Start a project

From the target Git repository, describe the project in a UTF-8 file. No manual
Project or issue creation is required:

```bash
.ai-team/bin/bootstrap-project --brief PROJECT.md --start
```

Use `--brief -` to read stdin, or omit `--brief` to open `$VISUAL`/`$EDITOR` (default
`vi`). Editor arguments are split safely; shell pipelines/expansions are not run.
`--title "My Project"` overrides the planner's Project title. GitHub authentication
(`gh auth login`, `gh auth refresh -s project`) and authenticated Claude CLI
are required for bootstrap planning. Set `HARNESS_PLANNER_PROVIDER="claude"` when
your coordinator uses another provider. Codex/Gemini bootstrap planners fail closed
until equivalent tool-free isolation is available; both remain supported as workers.
Existing CLI login sessions work; API keys are not mandatory. Do not place credentials in the brief: its validated plan is published
to GitHub issues. The GitHub repository must already exist, and autonomous workers
need a pushed baseline commit.

Repository defaults to `gh repo view --json nameWithOwner`, unless `HARNESS_REPO`
is set. `HARNESS_PROJECT_OWNER` defaults to its owner. A configured Project number
selects an existing Project; otherwise bootstrap reuses the unique open Project
with the requested title or creates one. Clear `HARNESS_PROJECT_NUMBER` to create
a new Project. The example owner/repository placeholders are ignored. Resolved
settings are written atomically to the local, non-secret runtime.env file.

The planner uses `HARNESS_PLANNER_PROVIDER` (default: coordinator provider) and
`HARNESS_PLANNER_MODEL`, falling back to the provider's STRONG, generic, then CLI
default model. Bootstrap's Claude process has no tools, runs in an empty private
temporary directory, disables customizations/session persistence, and receives
only an allowlisted environment without harness/GitHub/decision-engine secrets.
Its prompt travels through stdin. Every response must pass the bounded JSON/graph validator.

Bootstrap stores the full plan in a tracking issue before creating child issues.
Rerun the **same brief** after a failure: it reuses the durable plan and existing
issues, including closed issues, even when local runtime caches are deleted. Each
node's canonical issue number, URL and title/body digest is bound in the root.
Recovery adopts an unbound issue only if it was authored by the authenticated
publisher and exactly matches the planned content; spoofed markers or edited
contents fail closed. Dispatch rechecks the binding, content and native edges.
The complete envelope (plan, identity bindings and completion state) is authenticated
with HMAC-SHA256. `migrate-secrets` generates `HARNESS_BOOTSTRAP_HMAC_KEY` in the
external 0600 secret store on first use. Back up this store securely; runtime.env
contains configuration only and may point to it.
The signing key is never sent to the planner. Losing/changing it makes existing
signed roots unusable; the harness refuses to regenerate it when roots already
exist. Restore the original key to recover. Rotation requires a deliberate trusted
re-signing/migration procedure; automatic key rotation is not supported.

**Mode transition:** once a bootstrap signing key is configured, implementer
dispatch accepts only signed bootstrap task identities. Ordinary/manual issues
cannot run in that checkout, even if marked READY. There is no bypass switch.
This prevents removal of GitHub markers from downgrading tasks to unverified work.
Reviewer/security-reviewer packs remain supported. Legacy manual-task operation
is available only in repositories that have not enabled bootstrap signing.
Different brief contents intentionally produce a new bootstrap. Do not run the
same bootstrap concurrently from different clones: the lock is checkout-local.
Incomplete roots block all descendants. Only executable dependency-free leaves
become READY after graph verification; other leaves stay BLOCKED, containers stay
BACKLOG. Initial Provider/Model fields use the existing decision engine; the
coordinator rechecks capacity and routing at dispatch.

Worker prompts also travel via stdin. Claude disables session persistence. Codex
uses ephemeral sessions with a disposable writable `CODEX_HOME`, separate SQLite
state, and a least-privilege permission profile: model-generated commands can
read/write the assigned clone as required by the role but cannot read the copied
`auth.json`. Host Codex state and repository `.codex` configuration are masked.
Gemini has no equivalent switch, so workers use a private disposable
`GEMINI_CLI_HOME`, copy only OAuth credential/account files and remove the
temporary home on exit. Its custom user settings are intentionally not copied;
the supported Gemini worker authentication is the existing personal OAuth login.
Explicit worker result/evidence files remain part of the harness contract.

`--start` runs **one** deterministic coordinator cycle after publication. Continue
with `.ai-team/bin/coordinator-cycle` for subsequent lifecycle transitions. Each
cycle performs at most one signed, fresh-state-validated action for an item; the
broker owns implementation reconciliation, publication, review, merge,
finalization, and dependency unlock.
`--start-timer` currently fails before any publication: the existing systemd
installer uses global unit names and cannot safely manage multiple projects.
For continuous operation on a single project, use the existing timer instructions
below only after ensuring no other project owns those units.

In a legacy repository without bootstrap signing, the manually created issue flow
still works:

```text
Harness Status = PLANNING
```

The coordinator decomposes work, creates issues/dependencies and makes executable
dependency-free work READY.

## Decision layer

Rules mode:

```bash
HARNESS_DECISION_ENGINE="rules"
```

Jev mode:

```bash
HARNESS_DECISION_ENGINE="jev"
```

In Jev mode the coordinator builds compact task state and calls the harness
decision CLI. The result may look like:

```json
{
  "engine": "jev",
  "kind": "task",
  "decisions": {
    "provider": {
      "value": "codex",
      "source": "jev",
      "confidence": 0.86
    },
    "model_profile": {
      "value": "strong",
      "source": "jev",
      "confidence": 0.90
    },
    "review_mode": {
      "value": "security",
      "source": "jev",
      "confidence": 0.84
    }
  }
}
```

A low-confidence field becomes:

```json
{
  "value": "balanced",
  "source": "rules-fallback"
}
```

This is expected behavior.

## Important boundary

Jev never decides whether:

- CI is green
- dependencies are done
- a worker is alive
- a branch/PR exists
- a PR has merged
- merge gates are satisfied

Those remain direct deterministic checks.

## Worker lifecycle

```text
READY
-> route
-> isolated clone
-> implementer
-> local commit/evidence
-> coordinator publishes PR
-> independent review
-> CI
-> deterministic merge
-> DONE
```

## Switch Jev off

Edit runtime.env:

```bash
HARNESS_DECISION_ENGINE="rules"
```

No migration or Project change is required.
