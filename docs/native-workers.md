# Native worker lifecycle

`internal/worker` is the native coordinator integration boundary for Codex workers.
It does not replace the Python broker's `spawn-agent` entry point in this migration
step. Call `worker.New`, then `Manager.Start(ctx, Request{...})` after the
coordinator's signed dispatch/dependency checks. Always call `Handle.Stop` when
the unit completes or must be cancelled. The narrow `.ai-team/bin/worker-process`
adapter only invokes systemd; Go owns paths, policy, clone creation and cleanup.

Configuration supplies the canonical source checkout, a dedicated external
runtime root, the shared external secrets root (covering every repository), the external Codex authentication home,
and trusted absolute adapter/runner paths. Private roots must already exist with
0700 mode; auth.json and secret files must be regular 0600 files. Symlinked paths
are rejected. Runtime and credential roots must be disjoint. The runner normally
points to `.ai-team/bin/run-provider-agent` in the trusted coordinator checkout.
The current API launches Codex; adding other provider authentication contracts is
separate work.

A SHA-256-derived 128-bit namespace of the canonical checkout path scopes every
unit, claim, clone, job, task pack, result and disposable home. Separate checkouts
of the same remote intentionally have separate namespaces. An atomic mkdir claim
prevents simultaneous duplicate issue/role launches, including across coordinator
processes. Different repositories sharing a runtime root may use the same issue
number concurrently.

Clones use `git clone --no-local` followed by checkout of the assigned issue branch
from the source checkout's HEAD. The caller must supply a source checkout at the
intended implementation/review commit. No shared Git directories, commondir,
worktree registrations, alternates, symlinked/hard-linked metadata or Git config
includes/worktree redirection are accepted. Validation runs before launch.

The bubblewrap command mounts the host read-only, masks credential roots and all
worker runtime state, and exposes only the assigned clone (read-only for review),
disposable home, result, read-only pack and private /tmp. Auth and generated Codex
configuration are read-only mounts. Project Codex configuration is masked.
Both bubblewrap's network namespace and Codex's network permission disable network.
The subprocess environment is cleared, including systemd-manager inheritance.
This deliberately does not provide networked provider transport or dependency
fetching: a network-dependent provider invocation cannot complete inside this
boundary. Any future transport exception requires separate security design.

Stop waits for systemd termination before removing the disposable home and claim;
it retains the clone, pack, job and result for coordinator publication/review.
Existing retained state is never overwritten. The coordinator must archive/remove
it before retrying. A failed launch rolls back only after termination is confirmed.
If termination cannot be confirmed, state and claim remain for recovery: inspect
the namespaced job.json, stop its recorded unit using the adapter, confirm it is
inactive, then delete the disposable home and claim. Never release a live claim.
Automatic reconstruction/recovery belongs to the coordinator recovery workstream.

## Offline validation (CI safe)

```sh
go test ./internal/worker/...
go test -race ./internal/worker/...
```

Tests create real independent Git clones and fake authentication, record the launch
boundary without starting systemd, assert private modes and required isolation
flags, reject corrupt metadata before launch, test failure cleanup, and concurrently
run two repositories with issue 16 under a shared runtime root. These tests do not
claim to exercise kernel namespaces or live authentication.

## Opt-in local isolation smoke test

On a Linux workstation with a user systemd session, bubblewrap and Python 3:

```sh
HARNESS_WORKER_SMOKE=1 go test -v ./internal/worker -run '^TestLocalIsolationSmoke$'
```

This uses the real adapter and bubblewrap to probe denied network access, hidden
secrets, denied coordinator writes and permitted clone/result writes. It uses
fixture credentials and never runs in CI unless explicitly opted in. No fallback
to an unsandboxed process is allowed if the host lacks namespace support.

For the existing **authenticated Codex** permissions smoke procedure, locally run:

```sh
HARNESS_RUN_CODEX_SMOKE=1 bash .ai-team/tests/codex-bwrap-smoke.sh
```

Read that script's prerequisites first; it uses your external authenticated Codex
home and creates disposable credentials. It is opt-in, never a CI dependency, and
covers the existing provider permission boundary rather than claiming that an
online provider can function inside the native network-disabled namespace. No real
credentials are required for the native Go test suite. Hosted CI and independent
security review remain broker gates before merge.
