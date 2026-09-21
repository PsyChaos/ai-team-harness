# Live Factory Floor — issue #13

## Operation and observation contract

Build with `go build -o /tmp/harness ./cmd/harness`, then run:

```sh
/tmp/harness -journal /absolute/path/to/journal.json -repo owner/repo -project owner/7
```

The default listener is `127.0.0.1:8080`. Without `-journal`, the UI explicitly
reports an unavailable source; Demo stays off. The checkbox enables labeled,
local simulated observations only for the current page. Reload always returns
to live mode. The UI has no execution, publication, or mutation controls.

`-journal` selects a trusted operator-configured file, never an HTTP parameter.
The command reads the existing journal at startup and follows it once per second
without writing it. The source must preserve immutable, increasing journal IDs;
backwards cursors, rewritten retained events and corrupt input are rejected.
Read failures preserve the last observations, whose timestamps continue aging.
The follower accepts at most 512 retained events, one pending event, and 1 MiB
per event. Producers must retain within those limits. Restart the server to
select a different journal or reset source sequence. The one-shot `cmd/pybridge`
output can be used directly via its reported journal path; new source samples
require a producer to append to that same journal with consecutive IDs.

`dashboard.WithJournal` also mounts an in-process `journal.Handler` directly;
normal `Journal.Append` calls then notify SSE streams without disk polling.
Only `/snapshot`, `/events`, `/identity`, and embedded asset paths are served.
Requests cannot choose source files, commands, or API mutations. The listener's
existing authentication/deployment boundary remains operator-owned.

The UI consumes journal v1 `{version,cursor,events}` and named SSE `observation`
events (`{id,type,data}`). It subscribes after the snapshot cursor, ignores
replayed IDs, and refetches on sequence gaps, malformed records, reset events,
connection errors, or restored connectivity. It retains at most 200 events and
shows the latest 20 in activity rails. Snapshot replacement discards older
projection state, including tasks no longer in the retained window. Read-only
`/identity` reports the configured repository/project; the header also shows the
installation origin and observed repository/project identities. Projection keys
include repository, project and issue; missing identity remains unknown.

Supported observation payloads (all fields optional unless identified below):

- `pybridge.broker.observed`: existing bridge `repo`, `created_at`, `expires_at`.
- `pybridge.task.observed`: existing bridge `repo`, `issue`, `status`, `role`,
  `provider`. An assignment is shown with **unknown runtime**, even if its task
  status is IN_PROGRESS or DONE. The bridge intentionally excludes issue bodies
  and does not currently publish runtime or routing evidence; this change does
  not expand that source's data exposure.
- `dashboard.task.observed`: dashboard-safe task read projection with `repo`,
  `project`, `issue`, `title`, `body`, `status`, `role`, `provider`, `observed_at`,
  optional numeric `pr`, `pr_title`, `pr_body`, and optional `routing` evidence.
- `dashboard.agent.observed`: scoped by `repo`, `project`, `issue`, `agent_id`;
  `role`, `provider`, explicit `runtime_state`, `observed_at`, `expires_at`,
  `started_at`, `finished_at`, bounded public `activity`, and optional `routing`.
- `routing.decided`: task identity plus the native routing result fields
  `selected`, `decision_source`, `reason`, `policy_version`. Its selected model
  and effort are displayed as **requested**, never proof of effective execution.
  An observation may include `requested: {model,effort}` and independently
  confirmed `effective: {model,effort}`. This same shape is accepted under a
  task/agent's `routing` property. Task-wide decisions are not attributed to
  explicit agents/retries; their own observation must carry routing evidence.
  Missing effective values remain unknown.
- Other event types remain in the activity log with only `repo`, `issue`,
  `observed_at`/`created_at`, and `message`/`activity`/`reason`/`status` displayed.

These additive dashboard observation shapes are a read contract for future
producers, not claims that the current bridge supplies them. No model name,
effort, liveness, progress, private reasoning or cost is inferred. Timestamps
accept Unix seconds or ISO strings. Observations older than 120 seconds or past
explicit expiry are stale; missing/invalid/future timestamps are unknown.
Disconnection also overrides agent status with stale/unknown. Labels age every
15 seconds. Display strings are limited to 4096 characters and assigned through
DOM textContent. Supplied URLs and markup are ignored. Links are generated only
for validated `https://github.com/owner/repo/issues/N` or `/pull/N` identities.

## Original implementation validation (2026-09-21)

Provenance: implementation commit `9769c29ff993a0b32b9d9672dec8875480dfbef4`,
based on `6455033`. The commands below were reported by the original implementer
for that revision. They are historical local checks, not checks rerun for the
retry revision, hosted CI, or an exact-HEAD host attestation.

| Command | Result |
| --- | --- |
| `GOCACHE=/tmp/issue-13-go-cache go test ./...` | PASS; all Go packages, including dashboard boundaries and journal follower |
| `GOCACHE=/tmp/issue-13-go-cache go vet ./...` | PASS, exit 0 |
| `GOCACHE=/tmp/issue-13-go-cache go build -o /tmp/issue-13-harness ./cmd/harness` | PASS, exit 0 |
| `node scripts/check-dashboard-model.mjs` | PASS; routing provenance, unknown/stale, cursor validation/replay/gaps, bounded retention, repository/project isolation, constrained links |
| `node --check internal/dashboard/assets/app.js` | PASS, exit 0 |
| `node --check scripts/check-dashboard.mjs` | PASS, exit 0 |
| `GOCACHE=/tmp/issue-13-go-cache go test -race ./internal/dashboard ./internal/journal` | PASS; dashboard 1.020s, journal 1.279s, including immutable-source rejection |
| `git diff --check` | PASS, exit 0 |
| `/tmp/issue-13-harness` | BLOCKED, exit 1: `listen tcp 127.0.0.1:8080: socket: operation not permitted` |
| `node scripts/check-dashboard.mjs` | BLOCKED, exit 1: `ERR_MODULE_NOT_FOUND` for `playwright` |

Browser rendering/XSS behavior, screenshot review and hosted CI remain
**VALIDATION_PENDING**. No screenshots were produced in this worker. Passing
projection/URL tests do not substitute for rendered-XSS or browser assertions.

## External browser validation

The retry adds a required-to-pass `dashboard-browser` job to Harness CI on push,
pull request and manual dispatch. It provisions Node 22, Playwright 1.58.2 and
Chromium, builds the Go dashboard, starts the dashboard and reference servers,
waits for both to respond, and executes `node scripts/check-dashboard.mjs`.
Assertion failures fail the job (including through the log capture pipeline).
Server processes are stopped on exit. This is a workflow job, not a claim that
repository branch protection has been configured.

The `dashboard-browser-<github.sha>` artifact is retained for 14 days, even on
failure. It contains any generated screenshots/diffs/results plus browser output,
server logs, Node/Playwright versions and `source-commit.txt` from `git rev-parse
HEAD`. For pull-request runs this can identify GitHub's tested merge revision;
use that file when attributing evidence rather than assuming the branch head.
A successful job proves the browser assertions ran and captures were generated;
the diagnostic pixel difference has no pass threshold and visual comparison
still requires review. CDN access for the preserved reference is required.

In an environment with Playwright, Chromium and loopback sockets provisioned,
serve the application and serve the preserved repository theme on port 8081:

```sh
python3 -m http.server 8081 --bind 127.0.0.1
node scripts/check-dashboard.mjs
```

The browser test mocks `/snapshot`, `/identity`, and the named EventSource
stream; it verifies all three tabs, task details, requested/effective routing,
live updates, stale/unknown states, reset/reconnect/offline behavior, Demo
isolation/default-off, unavailable-source handling, and malicious issue/PR/log
text. It checks no injected elements or script execution occur and asserts the
exact generated GitHub links. This is frontend integration validation, not an
authenticated production-source attestation. The Go journal tests separately
exercise real HTTP SSE framing and replay.

The script saves desktop (1440×1000) and mobile (390×844) Factory/Agents/Events/
detail screenshots in `/tmp/issue-13-screenshots`. The original theme requires
its React CDN resources in that environment. Factory reference pixel diffs are
diagnostic; Agents/Events reference screenshots are also captured for manual
comparison. Review palette, panel hierarchy, typography, focus and mobile
spacing. The original reference has no task-detail view, so compare that panel
against the reference's visual language rather than asserting pixel equality.
Both original theme files are preserved. Record browser results and visual
review evidence before acceptance; publication/review does not authorize merge.

## Retry validation (2026-09-21)

Source: retry working tree based on
`9769c29ff993a0b32b9d9672dec8875480dfbef4`; changes are limited to the browser CI
job, waiting for asynchronous identity loading in its browser test, and this
report. No dashboard production code changed. The rejecting review's CI option
is implemented, but no hosted run or browser attestation was supplied locally.

| Retry check | Result |
| --- | --- |
| `node scripts/check-dashboard-model.mjs` | PASS, exit 0: projection, routing provenance, unknown/stale, cursor recovery, bounds and constrained links |
| `node --check scripts/check-dashboard.mjs` | PASS, exit 0 |
| Python `yaml.safe_load` of `.github/workflows/harness-ci.yml`, assertions for browser command/artifact wiring, and `bash -n` on every new job's `run` step | PASS, exit 0: workflow YAML, browser execution/artifact wiring and Bash syntax |
| `git diff --check` | PASS, exit 0 |

Local infrastructure probes reconfirmed `ERR_MODULE_NOT_FOUND` when importing
`playwright`, and `PermissionError: [Errno 1] Operation not permitted` when
creating a loopback socket. No browser was launched and no screenshots were
produced here. Browser/XSS execution, screenshot review, and hosted CI remain
**VALIDATION_PENDING**, to be supplied by the coordinator-run CI job and review
of its artifacts. Historical test results above do not validate this revision.
