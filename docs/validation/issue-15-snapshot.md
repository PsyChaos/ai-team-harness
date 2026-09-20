# Issue 15: native signed snapshot/apply

`internal/snapshot` implements the bootstrap and broker HMAC-SHA256 encoding:
UTF-8 JSON, sorted object keys, compact separators, Python string escaping,
and lowercase hexadecimal signatures. Only the top-level `signature` is omitted
from envelope authentication. Broker action IDs authenticate `{nonce, payload}`
with the `a_` prefix and omit only the action's `id` from its payload.

Construct separate `Signer` instances using the existing bootstrap and broker
keys. `NewSigner` requires exactly 64 lowercase hexadecimal characters; nil and
zero-value signers fail closed. No keys are loaded, created, rotated or logged by
this package. Test keys are public fixtures, never installation credentials.

## API and integration boundary

- `Sign`: trusted producer signing; never an untrusted-input approval endpoint.
- `Verify`: authenticates all fields; does not validate graph schema or authority.
- `Migrate`: verifies before canonical re-encoding, preserving every field and
  the signature. It neither republishes nor changes the durable graph. Python's
  existing `read_envelope` still validates plan schema when loading the result.
- `SignSnapshot`: creates action IDs and signs a trusted broker snapshot, then
  validates identity, timestamps and action integrity.
- `Validate`: verifies broker signature, repository/version, Python time-window
  boundaries, allowed action kinds and unique nonce-bound action IDs.
- `Apply`: validates snapshot and all selected IDs/handlers before calling any
  handler, preserves decision order, and stops at the first failure with a count
  of successful calls. It rejects unsigned input even for empty selections.

The executor must supply handlers that perform the existing authoritative
re-reads, locks, policy and lifecycle checks. Authentication does not replace
those gates or provide replay protection. Multi-action execution is not atomic;
a handler can have effects before returning an error. Cancellation stops later
calls. This package does not switch the live executor or implement lifecycle
handlers belonging to subsequent tasks; Python remains the active executor.

## Compatibility and migration

Tests consume `internal/domain/testdata/bootstrap.json` and `snapshot.json`,
recorded through actual bootstrap/broker producers (see that directory's
`README.md` and capture script). They are not hand-authored signatures.
`TestMigrationBackToPython` migrates both through Go and invokes the actual
Python `read_envelope` and `validate_snapshot` on the result. Every JSON field,
including graph bindings, is compared before and after migration. Python also
produces Unicode/control-character and large-integer signature evidence during
this test. Native action ID generation reproduces the complete recorded broker
snapshot byte-for-byte in canonical form.

The native format covers the existing integer-only graph/snapshot contracts.
It rejects floating-point numbers, malformed UTF-8, unpaired surrogates,
duplicate object keys, depth over 100 and documents over 4 MiB rather than
silently changing their signed meaning. Unknown fields of supported JSON types
are retained and authenticated. JSON presentation whitespace is normalized;
the canonical signing bytes, signature and full value tree are preserved.
Rollback needs no key or graph transformation: Python reads the migrated data.
Python 3 is required for the cross-verification tests, not for production Go.

## Local security evidence checklist

These are implementer checks, **not independent security approval**.

- [x] Key format and missing/zero-value key handling fail closed.
- [x] Constant-time HMAC comparisons cover envelope and action signatures.
- [x] Real Python-signed fixtures verify; unsigned, changed, uppercase and wrong-key signatures fail.
- [x] All fields, including unknown extensions, remain authenticated.
- [x] Ambiguous JSON and unsupported numeric forms fail closed.
- [x] Durable graph migration retains the complete value tree and Python verification.
- [x] Stale, future, wrong-repository/version and altered/duplicate action IDs fail.
- [x] Invalid selections and missing handlers produce no calls; handler failure and cancellation stop subsequent calls.
- [x] No publishing, credential access, new dependencies or live executor cutover.
- [ ] Independent security reviewer: inspect these boundaries, rerun tests,
      record decision/findings against the published commit and attach to PR.
- [ ] Hosted CI against the published commit.

The coordinator owns publication and independent reviewer dispatch. This document
is the review checklist to attach to the PR, not a claim that review has occurred.

## Validation commands

```sh
go test ./internal/snapshot/...
go test -race ./...
go vet ./...
go build ./...
python3 -m unittest discover -s .ai-team/tests -p 'test_*.py'
```

Local results (Go 1.27.0 linux/amd64): targeted snapshot tests passed
(`0.051s`, 90.6% statement coverage); `go test -race ./...` passed all packages;
`go vet ./...` and `go build ./...` exited 0. The Python suite passed all 80 tests
in 30.875s. `git diff --cached --check` reported no whitespace errors.
Hosted CI and independent approval remain required before merge.
