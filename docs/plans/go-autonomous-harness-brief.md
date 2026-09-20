# Go Autonomous Harness and Factory Floor Dashboard

## User request and delivery

Evolve this repository, PsyChaos/ai-team-harness, using its own signed GitHub task
graph and isolated implementation/review lifecycle. The user wants the decision
engine to choose which eligible task runs, its provider, concrete model and
reasoning effort, a Go-based runtime, and a live dashboard matching the supplied
theme. This brief authorizes implementation, testing and independent review of
the resulting tasks. GitHub remains durable work state. Start useful independent
foundation tasks immediately after the entire graph has been published.

Create one coherent project, approximately 18-22 executable tasks in 4-5 phases
(at most 28 nodes including containers). Each leaf should fit one reviewable PR.
Use real native dependencies, precise acceptance criteria and concrete tests.
Do not create a separate audit task for every requirement. Put at least two
useful independent tasks on the initial frontier: Go foundation/contracts and
the theme-faithful dashboard shell. Avoid overlapping ownership between these.
No task should require the full rewrite before producing a verifiable result.

## Existing repository and operating context

- Current implementation: Python broker `.ai-team/coordinator/broker.py`, Python
  bootstrap `.ai-team/bootstrap/bootstrap.py`, decision engine
  `.ai-team/decision/decision_engine.py`, shell entrypoints `.ai-team/bin/`.
- Existing signed snapshot/apply, independent clones, systemd/bubblewrap workers,
  external 0700/0600 secrets, disposable Codex homes, strict structured results,
  independent reviews, CI and head-bound merge gates are working contracts.
- Existing suite: 64 Python tests plus shell secret/provider boundary checks.
  Keep these running during migration. Real authenticated isolation smoke tests
  stay opt-in and local; CI must not require provider credentials or systemd.
- Current router chooses provider and fast/balanced/strong capability profile.
  A profile is not a concrete model or reasoning effort. The Python broker still
  reads stored routing fields; runtime eligibility-aware scheduling and concrete
  model/effort enforcement are part of this project, not existing functionality.
- `codex` 0.154.0 and Claude Code 2.1.235 are installed. Gemini is not installed;
  missing/unauthenticated providers must be unavailable, never silently selected.
  Go 1.27.0 is installed. Verify version/tool capabilities locally when needed;
  do not invent model IDs, prices, quotas, supported efforts or CLI flags.
- Runtime workers have network disabled and can write only their independent
  clone plus disposable outputs. Prefer standard-library Go and native browser
  HTML/CSS/JavaScript modules so initial work has no package-download dependency.
  If an external dependency is justified, specify a trusted provisioning step
  and offline cache rather than weakening worker network or credential isolation.
- Scope is this repository only. Vensift is a separate consumer: its existing
  timer is paused and its issues/results must not be changed by this project.

## Product architecture and migration

Build a native Go control plane with a `cmd/harness` CLI and cohesive `internal/`
packages. Keep public interfaces documented and small. Prefer standard library;
do not introduce a second orchestration framework or rewrite tools that already
provide suitable OS isolation. Shell may remain a narrow process-launch adapter.

The Go system must eventually own configuration, GitHub gateway, deterministic
state reconstruction, scheduling, signed snapshot/apply, worker lifecycle,
review/publication/merge gates, event journal and HTTP dashboard API. A Python
subprocess bridge can be an explicitly temporary migration mode but cannot count
as completing the Go port. Preserve bootstrap signatures and durable existing
graphs or provide an explicit compatibility adapter and tested migration.

Migration stages: (1) foundation and contract fixtures; (2) routing, readers and
observer UI; (3) native execution/lifecycle parity and shadow comparisons;
(4) controlled cutover, recovery and self-improvement loop. The existing Python
runtime remains the active executor until native parity tests and a disposable
end-to-end rehearsal pass. Do not delete the legacy fallback before that gate.

The active executor must be pinned to a reviewed revision, separate from the
source under development. Merging a PR must not silently hot-reload the running
coordinator or change its policy. Stage a versioned release, compare/shadow it,
activate only at a quiescent checkpoint with a lock, and retain a rollback path.

## Scheduling and provider/model/effort selection

1. Build an eligible frontier from exact dependency state, issue state, active
   leases, retry limits, file/resource conflicts, capacity and provider health.
   Ranking cannot override eligibility. Prevent starvation with bounded aging;
   finish reviews/recovery before expanding implementation work.
2. Maintain a configurable provider/model capability registry: provider CLI and
   auth availability, enabled models, allowed efforts, work capabilities,
   concurrency and optional budgets. Record discovery time/source. CLI defaults
   must be identified as defaults, not labeled as a specific invented model.
3. Select a concrete tuple: issue + role + provider + model + effort, along with
   capability profile, decision source, reason and policy version. Decide from
   task complexity/risk/work type, capabilities, availability and observed past
   results. Configured budgets are hard constraints; missing cost/quota data is
   unknown, not zero. Do not promise the cheapest or best model without evidence.
4. Implement deterministic rules as the baseline and fallback. Support a typed,
   bounded optional judgment adapter (compatible with the existing Jev use case)
   for ranking among already-eligible choices. Timeout, malformed response, low
   confidence or unavailable adapter falls back explicitly and audibly.
5. Enforce the selected tuple at the provider process invocation, using verified
   provider-specific flags/config. Distinguish requested and effective settings.
   Unsupported effort/model combinations fail before spawn or use a documented
   policy-approved alternative with evidence; no silent drops or substitutions.
6. Persist routing evidence against the exact issue/attempt/head where relevant
   and expose it to the dashboard. Independent reviewer selection must preserve
   different sessions and prefer a different available provider.
7. Test representative small/simple, high-risk, scarce-capacity, disabled model,
   unsupported effort, stale health, provider outage and retry/provider-switch
   scenarios. Use fake CLI adapters and recorded contracts without credentials.

## Factory Floor dashboard

The design source is `theme/Factory Floor.dc.html` and `theme/support.js`.
Preserve these reference files. The reference is a custom HTML/React preview,
not a production application; replace mock simulation with actual observable
runtime data and a maintainable UI. Prefer native browser modules plus HTML/CSS,
served/embedded by the Go binary; a new JS framework is not required.

Keep the supplied dark factory/control-room visual direction: #0b0d11 background,
subtle blue/green accents, compact bordered panels, Archivo-like UI typography,
IBM Plex Mono-like metrics, Factory/Agents/Events tabs, control tower, live task
flow, agent workstations, queue/review/merge areas and compact status HUD. Fonts
need local/system fallbacks; runtime must work without Google Fonts/CDN access.
Preserve the visual hierarchy and meaningful micro-interactions, respect reduced
motion, support keyboard navigation and readable small-screen layouts.

Required live views:
- Factory: project/repository identity, cycle state, queued/running/reviewing/
  blocked/done counts, dependency flow, available worker slots, failed attempts,
  sync freshness and connection state.
- Agents: current issue/title/link, role, provider, requested/effective model and
  effort, task/branch/PR, runtime state, start/elapsed/heartbeat times and bounded
  current activity. Show observed tool/activity events or explicit unknown/stale
  status; never invent progress percentages, private reasoning or token costs.
- Events: ordered, filterable lifecycle and routing events, retry causes,
  review/CI/merge outcomes, sequence cursor and timestamps. No credentials,
  private Codex home paths, raw auth material or unbounded prompt dumps.
- Task detail: scope/acceptance links, dependencies, attempts, assignment reason,
  validation/review/CI status and links to durable GitHub evidence.
- Repository/project selection and clear labels so separate installations are
  never conflated. Default local-only HTTP binding at 127.0.0.1.

Implement a documented read model with event IDs and a bounded persisted journal,
HTTP snapshot endpoints and SSE streaming. Reconnect/resume, duplicate/out-of-order
events, retention, backpressure and full resync must be tested. Prefer observing
existing broker snapshots/GitHub/systemd state first, then replace the adapter
with native Go events without changing the UI contract. API reads must not expose
arbitrary files, execute commands or mutate Project state. Remote/mutating controls
are out of scope for the first release; labels must not imply inactive controls
are functional. Escape all issue/PR/log text and constrain links.

Ship a deliberate demo fixture mode for visual/E2E tests, visibly labeled Demo,
separate from real live data. A production screen must never silently fall back
to simulated successful agents. Validate appearance against the supplied theme
with desktop/mobile screenshots and test Factory/Agents/Events navigation, task
detail, stream updates, reconnect/offline states and sanitized malicious content.

## Safe autonomous self-improvement

Add a bounded improvement loop based on failed attempts, recurring error classes,
review outcomes, CI failures and measured routing results. It should generate
deduplicated proposals with evidence, expected benefit and validation criteria,
publish them through the signed planning path, and execute only allowed work
types within configured concurrency, retry, time/cost and iteration limits.
Start with fixture-driven evaluation and a small documented opt-in allowlist for
internal quality/performance/bug fixes. No self-expanding permissions, unsigned
tasks, secret access, policy weakening, hidden spending or bypassed review/CI.
Prevent recursive improvement floods; require cooldowns and stable deduplication.
Any model calibration/routing policy proposal needs replay/evaluation against a
versioned dataset and a reviewed change. Expose recommendations, decisions,
budget use/unknowns and stop reasons in the dashboard.

GitHub remains authoritative for task/review/merge decisions; the local journal
is reconstructible observation state. Crash/reboot recovery must not duplicate
workers, comments, PRs or merges. Namespaced locks, unit IDs, job files and paths
must isolate multiple repositories, including colliding issue numbers. Preserve
the independent-clone contract and reject .git/commondir and unsafe metadata.

## Suggested work packages (decompose coherently, preserving dependencies)

Go foundation/domain/config/contracts; visual shell/theme mapping; provider
capability registry; deterministic scheduler and routing; optional judgment and
evaluation fixtures; model/effort invocation adapters; Go GitHub readers and
state reconstruction; signed snapshot/action compatibility; event journal/SSE;
live Python observer adapter; dashboard live binding/details; namespaced worker
execution and recovery; native implementation publication and review lifecycle;
native CI/merge/dependency gates; bootstrap/signature migration compatibility;
bounded improvement proposal loop; shadow/parity and versioned activation/rollback;
browser/security acceptance and operator installation/docs. Split a large package
when needed, without exceeding the bounded project size or inventing busywork.

Mark authentication/secret/worker isolation, signed actions, publication/merge
and runtime activation HIGH risk so independent security review is required.
The native routing registry/scheduler and visual shell can be MEDIUM if bounded
to fixtures/read-only behavior. Include Go tests/race/vet and legacy tests in CI;
do not replace offline tests with real paid provider calls.

## Project completion criteria

A local operator can start the Go binary, inspect real assignments and activity
in the supplied Factory Floor design, explain each task/provider/model/effort
decision, and observe one signed task from eligible frontier through isolated
execution, independent review, passing CI and gated merge. A tested opt-in
self-improvement proposal can follow the same path without changing its own
authority. Installation, restart recovery, multi-repo isolation, migration and
rollback are documented and exercised. Report implemented vs remaining behavior
honestly throughout the migration.
