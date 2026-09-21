# Native publication and review assignment

`Service.Publish` accepts a coordinator-bound repo/issue/attempt/head, the signed
bootstrap envelope, the worker's Markdown result, and trusted available review
sessions. It verifies the bootstrap signature using `snapshot.Signer`, rejects
incomplete graphs, invalid references/cycles and non-executable issue bindings,
and validates the existing Python result contract. `VALIDATION_PENDING` remains
pending; publication does not approve CI or merging.

Reviewer selection excludes the implementation session, unavailable sessions and
expired availability evidence. In configured candidate order it prefers another
provider, falling back to the same provider only with a distinct session. The
assignment records both identities, candidates, decision reason, graph/result
digests, PR and exact repo/issue/attempt/head.

`NativeHost` uses argument-array `git` and `gh` calls to verify clone state and
base ancestry, push the immutable commit without force, and create/recover an
open PR with the exact head. A remote branch at another head fails closed and
requires coordinator reconciliation. It writes immutable routing evidence to an
issue comment before launch; identical retries reuse the comment, while
conflicting evidence fails closed. It rechecks the open PR's head before launch.

This is a coordinator library, consistent with the existing native snapshot and
worker packages; it does not switch the current Python execution CLI to Go.
The coordinator must supply `Check` (serialized action authorization, fresh
publisher/native graph edge checks, isolated clone metadata/config/hook
sanitization) and `Launch` (idempotent reserved-session worker dispatch). Neither
callback may come from task data. Candidate availability must include capability,
authentication and capacity checks. Hold the lifecycle lock through the entire
service call. On retry, recover the persisted assignment and its candidate
snapshot; do not generate a replacement session under the same identity.
`CommandRunner` needs a trusted directory and explicit coordinator environment.
No credentials or worker-controlled environment are implicitly inherited.

Security review dispatch and CI/merge gating remain separate coordinator duties.
The standard reviewer assignment is not a security approval.

Validation: `go test ./internal/lifecycle/publication/...`. The fixtures exercise
signature rejection before side effects, reviewer diversity/fallback, exact-head
binding, durable-before-dispatch ordering, Git/gh command execution via a recording
transport, immutable assignment replay, and schema parity against the current
Python broker parser. Python 3 is required for the schema oracle; it is never
invoked by production publication code. Real authenticated GitHub publication and
worker launch require host validation; the fixtures do not claim those ran.
