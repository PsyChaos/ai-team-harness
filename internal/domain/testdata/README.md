# Recorded Python contracts

Captured from Python source revision `250e5df98c26a03857632d192fee7e08d74dee5d`.
These are actual outputs of the current modules run with the repository's offline
integration adapters, not hand-authored approximations or production exports.
No provider credentials, live GitHub access, or systemd are used.

Run from the repository root:

```sh
python3 internal/domain/testdata/capture.py
```

`--output DIRECTORY` records elsewhere for comparison. CI regenerates and diffs
every fixture, making drift in the Python contracts visible.

| Fixture | Production path exercised | Controlled inputs |
| --- | --- | --- |
| bootstrap.json | bootstrap-project subprocess, bootstrap(), read_envelope() | BootstrapTests setup and persistent fake GitHub/planner; existing phase/first/next plan |
| project-item.json | normalize_project_item(), normalize_blockers() | Existing graphql_item() and blocker_node() test helpers |
| snapshot.json | make_snapshot(), lifecycle_action(), action_payload(), validate_snapshot() | Existing item()/issue_scope() helpers; GitHub reads, clock, nonce and local artifact locations controlled |
| routing-task/retry/action.json | decision_engine.py command-line rules backend | Existing .ai-team/examples/decision-*.json inputs |
| routing-judgment.json | jev_retry(), choice_answer(), gated() | Existing retry example with one available provider; typesafe_call replaced by a fixed judgment response |

Snapshot and bootstrap signatures use the public test key `ab` repeated 32 times.
The snapshot time is 1700000000 and its nonce is `01` repeated 16 times. These
fixtures are expired test evidence, never executable authority. The judgment
model name is a test label, not a configured provider model.

Round-trip tests compare the complete JSON value tree, preserving absent fields,
explicit nulls, empty objects/arrays, empty digests, dependency state, graph
bindings and routing evidence. This does not assert byte-level compatibility
with Python's canonical HMAC encoding or implement signature verification in Go.
GraphQL field nodes intentionally remain raw JSON because GitHub field types
have heterogeneous schemas. The fixtures cover the current observed variants;
future contract changes must update the producer capture and typed records.
