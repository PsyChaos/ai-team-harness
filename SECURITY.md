# Security

## Coordinator authority boundary

`coordinator-cycle` invokes only the deterministic broker. No LLM receives
GitHub mutation, Git publication, systemd, shell, or coordinator-secret access.
Signed snapshots expose only opaque allowlisted action IDs and are revalidated
against fresh state before every mutation. Direct `project-set-field` calls are
denied.

Secrets are outside the repository in `HARNESS_SECRETS_FILE` (parent mode 0700,
file mode 0600). `load_runtime` always scrubs secret variables; only privileged
broker/bootstrap/decision entrypoints call `load_secrets`. Run
`.ai-team/bin/migrate-secrets` once after upgrading from a legacy runtime.env.
Worker transient units enter a bubblewrap mount/PID namespace with a read-only
host root and explicit writable binds only for their assigned isolated clone and
individual result file. Each clone has its own `.git`; main/other-task Git refs,
config, hooks and objects are not writable. The namespace replaces the
entire external secret directory and user runtime directory with private tmpfs
mounts. A nested `systemd-run --user` therefore cannot reach the manager to escape
the namespace. The shared cross-repository harness secret root, custom store,
GitHub CLI config, SSH/GnuPG directories and plaintext Git credential files are
masked. Workers also receive no coordinator secret environment variables. Codex
receives a disposable writable `CODEX_HOME` containing only a read-only copy of
`auth.json`, an empty harness-owned configuration, and fresh runtime/SQLite
directories; the host `~/.codex` remains masked and host state is never copied.
Codex's parent process can authenticate, while model-generated commands run under
a permission profile limited to minimal platform paths and the assigned clone.
An exact deny rule prevents those commands from reading the disposable
authentication home. Repository `.codex` configuration is masked so hostile
project configuration cannot select a weaker sandbox. Provider authentication
remains in the provider process's memory and private home; for threat models that
include compromise of the provider binary itself, use a dedicated OS
account/container with narrowly scoped credentials.

Implementers necessarily write their isolated clone's Git objects and refs. The
broker therefore treats that clone's `.git` as hostile after the worker exits: a
broker-owned external manifest binds repository, branch, object format, GitHub
default branch, and exact pre-dispatch base SHA; metadata
symlinks, special files, hardlinks and oversized trees are rejected; hooks,
alternates and worker config are removed/replaced. Privileged Git inspection pins
an empty trusted hooks directory, disables fsmonitor/external diff/textconv and
unsafe protocols, resets local credential configuration, and publication uses the
manifest's exact validated GitHub origin rather than a worker-controlled remote.
Worker-controlled remote/replacement refs, grafts, shallow markers and generated
commit graphs are discarded; the canonical remote ref is reconstructed from the
bound SHA. Ancestry and diff validation use that SHA directly, never `origin/HEAD`,
and privileged Git runs with replacement objects disabled.

The broker owns the lifecycle from READY dispatch through reconciliation,
publication, independent review, fresh CI/head gates, merge, DONE, cleanup, and
dependency unlock. Worker/reviewer text is never sourced or converted to a shell,
Git, or GitHub command. Review evidence is HMAC-authenticated and bound to role,
provider, PR, and exact head SHA. Merge uses `--match-head-commit` and has no
`--admin` path. The legacy LLM coordinator is not a fallback.

## Provider isolation

Claude harness sessions use OAuth-compatible `--safe-mode`. This disables custom
instructions, skills, plugins, hooks and MCP servers while retaining normal login,
model selection, built-in tools and permissions. It is not a filesystem sandbox.
Role instructions are injected directly.

Claude worker sessions use `--permission-mode dontAsk` with role-scoped
`--allowedTools` rules. Unmatched actions are denied instead of waiting for an
interactive approval. Reviewers receive isolated-clone-scoped read-only inspection;
implementers additionally receive scoped file editing, validation commands and
local Git add/commit permissions inside their assigned clone. Neither path enables a
bare `Bash` rule or `--dangerously-skip-permissions`. See the official
[Claude Code permission rules](https://code.claude.com/docs/en/permissions).

Bootstrap planning additionally disables every tool, uses an empty private working
directory and an allowlisted environment, reads the brief from stdin, and disables
session persistence. Bootstrap currently requires Claude; Codex/Gemini remain
available for implementation/review. Worker prompts also use stdin; Claude disables
session persistence, Codex uses ephemeral sessions plus its least-privilege
permission profile, and Gemini uses a disposable private CLI home with only OAuth
credentials copied into it.

Bootstrap plans, completion state and issue bindings are HMAC-authenticated with
`HARNESS_BOOTSTRAP_HMAC_KEY` in the external secret store. Back it up securely.
Projects with a configured signing key allow only signed implementer tasks;
removing GitHub markers cannot downgrade them into ordinary unsigned work.

## Jev data boundary

When Jev is enabled, the compact `state` supplied to `.ai-team/bin/decide` is sent
to the configured TypeSafe API endpoint.

Do not put in routing state:

- passwords
- tokens
- API keys
- private keys
- unnecessary customer/personal data
- full source trees when a short task description is enough

## TypeSafe key

Keep `TYPESAFE_API_KEY` only in the external file named by:

```text
HARNESS_SECRETS_FILE
```

Never put it in:

- harness.yaml
- GitHub issues
- PR comments
- task packs committed to Git

## Decision authority

Jev cannot bypass:

- branch protection
- CI
- dependency gates
- human gates
- retry limits
- provider availability
- merge policy

A Jev result is routing judgment, not authorization.

## Higher assurance

Use a dedicated OS account/container/VM for unattended autonomous work and expose
only the repository and credentials that are actually required.
