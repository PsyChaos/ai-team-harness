# Bootstrap signature and durable graph migration

The Go reader preserves the Python v1 format. **No durable rewrite or key
rotation is required.** `bootstrap-migrate` authenticates a tracking issue's
embedded envelope using the existing `HARNESS_BOOTSTRAP_HMAC_KEY`, checks its
repository and root marker binding, and returns the entire body byte-for-byte.
It never calls GitHub, signs new data, or modifies an input file. Native parent
edges, dependencies, issue identity, node digests, Project membership and fields
remain in GitHub unchanged. Unknown envelope fields are authenticated and kept.
Incomplete graphs remain incomplete.

This is a compatibility migration, not a Go executor cutover. Python remains the
active bootstrap/executor. Signature verification does not establish plan
validity, trusted publisher, node identity or native relationship correctness;
retain the existing Python dispatch checks and lifecycle gates. Do not enable a
new executor on the strength of this command alone.

## Operator procedure

1. Pause bootstrap/coordinator writers and let active writes finish. Retain the
   deployed Python revision and securely backed-up existing bootstrap key. Do
   not create a replacement key. Broker and bootstrap keys remain separate.
2. Using the existing read-only export process, archive the root and all bound
   issues (including exact bodies/authors/URLs), native parent/dependency edges
   with complete pagination, and Project membership/field values. Record repo,
   root number and deployed revision. Store this outside disposable runtime
   state with restricted access. Save the exact tracking body as `root.before.md`.
   This command consumes that body, not an issue JSON export. Do not overwrite
   the source with shell redirection. Keep the archive through rollback checks.
3. Load the original bootstrap key through the existing private secrets loader
   and export `HARNESS_BOOTSTRAP_HMAC_KEY`; never put it in command arguments or
   logs. Build and run from repository root (substitute the actual repository):

   ```sh
   go build -o /tmp/bootstrap-migrate ./cmd/bootstrap-migrate
   umask 077
   /tmp/bootstrap-migrate --repo owner/repository < root.before.md > root.candidate.md
   cmp root.before.md root.candidate.md
   ```

4. Require both commands to succeed. Re-read the durable graph while writers
   remain paused and compare it to the archive; abort on drift or incomplete
   exports. Run the existing Python `--check-dispatch` path for executable
   leaves with the original identity/key context, retaining its native-edge and
   publisher checks. Do not publish the candidate: it is identical to the source.
5. Keep the Python executor active, discard the temporary candidate and resume
   the paused writers. A future Go executor deployment must pass its own gates.

## Failed attempt and rollback

On any verification, comparison or probe failure, stop the procedure and leave
writers paused. Discard the candidate (an output error can leave a partial local
file). Restore any local staged export from the untouched archive, compare bytes,
restore the previous reader selection/deployed revision if one was staged, and
verify Python dispatch with the original key before resuming. No compensating
GitHub writes are needed: this procedure performs none. Do not restore old GitHub
records over concurrent work; unexpected durable drift requires reconciliation.
Missing keys or already altered source records cannot be repaired by re-signing.

## Compatibility limits

The inherited Go HMAC reader accepts the existing integer-only JSON contract,
including exact large integers and supported unknown fields. It fails closed on
floats, duplicate keys, invalid UTF-8/unpaired surrogates, excessive depth (over
100), invalid signatures and oversized input (tracking body over 4 MiB here).
Missing/ambiguous plan markers and repo/root mismatches also fail. Unsupported
records remain untouched and must stay on the Python path pending an explicit
format decision. No lossy conversion or schema upgrade is attempted.

## Reproducible offline rehearsal

Prerequisites: Go and Python 3. No GitHub credentials or provider are needed.
The recorded graph is real Python producer output using offline adapters; see
`internal/bootstrap/migration/testdata/README.md` for provenance.

```sh
go build -o /tmp/bootstrap-migrate ./cmd/bootstrap-migrate
python3 internal/bootstrap/migration/testdata/exercise.py /tmp/bootstrap-migrate
go test -v ./internal/bootstrap/migration/...
```

The exercise compares every durable record and the full root body/signature,
checks both leaves with the original Python dispatch implementation, rejects a
wrong-key attempt without output, injects a failure after staging, restores the
pre-migration archive byte-for-byte, and checks Python dispatch again. The Go
test runs this same exercise and fails if any step fails. This proves the local
procedure; it does not claim a live GitHub migration or hosted CI approval.
