# Factory Floor Demo — issue #4

Run `go run ./cmd/harness` and open `http://127.0.0.1:8080`. Use
`-listen 127.0.0.1:8090` to change the port. `go build ./cmd/harness` produces a
standalone binary; it needs no assets on disk, frontend build, fonts, CDN or
package downloads. The server exposes only `/`, `/app.js` and `/styles.css`.

The committed HTML is a static adaptation of the preserved theme template with
fixed demonstration values. Its inline panel styling follows the reference;
`styles.css` adds responsive layout, keyboard focus and reduced-motion rules.
`app.js` only changes tabs and filters fixture events. No timers, API, SSE or
remote operations are present. All views share the visible Demo designation.
Archivo and IBM Plex Mono use installed fonts when available, otherwise system
sans-serif/monospace. Both original theme files remain unchanged.

## Validation in the implementation worker

- `GOCACHE=/tmp/issue-4-go-cache go test ./...`: PASS, dashboard package 0.006s.
  Checks embedded assets, MIME types, no external URLs or unexpanded templates,
  read-only methods, HEAD behavior, and rejection of unrelated paths.
- `GOCACHE=/tmp/issue-4-go-cache go vet ./...`: PASS.
- `GOCACHE=/tmp/issue-4-go-cache go build -o /tmp/issue-4-harness ./cmd/harness`: PASS.
- `node --check internal/dashboard/assets/app.js`: PASS.
- Running `/tmp/issue-4-harness`: BLOCKED by worker sandbox,
  `listen tcp 127.0.0.1:8080: socket: operation not permitted`.
- Chrome launch: BLOCKED, launcher target `/opt/google/chrome/google-chrome`
  is absent. Browser rendering, screenshot comparison, keyboard navigation and
  actual offline request observation were **not executed** in this worker.

## Browser validation to complete in a provisioned environment

### Retry evidence (2026-09-20)

The retry worker still cannot complete browser acceptance. The following checks
were rerun against the existing implementation and the expanded browser script:

| Command | Result |
| --- | --- |
| `GOCACHE=/tmp/issue-4-go-cache go test ./...` | PASS; dashboard 0.006s; command has no test files |
| `GOCACHE=/tmp/issue-4-go-cache go vet ./...` | PASS, exit 0 |
| `GOCACHE=/tmp/issue-4-go-cache go build -o /tmp/issue-4-harness ./cmd/harness` | PASS, exit 0 |
| `node --check scripts/check-dashboard.mjs` | PASS, exit 0 |
| `node --check internal/dashboard/assets/app.js` | PASS, exit 0 |
| `git diff --check` | PASS, exit 0 |
| `git diff 3e9f1a6 --exit-code -- 'theme/Factory Floor.dc.html' theme/support.js` | PASS; references unchanged |
| `/tmp/issue-4-harness` | BLOCKED; `listen tcp 127.0.0.1:8080: socket: operation not permitted` |
| `node scripts/check-dashboard.mjs` | BLOCKED; `ERR_MODULE_NOT_FOUND` for `playwright` |

The browser script now checks viewport overflow and reduced motion on all three
views, keyboard focus and visible outlines on selected tabs, arrow wrapping,
Home/End, focus on each event filter, reverse traversal and Space activation.
It also checks for external requests and page errors after interaction, rather
than only on initial load. These new browser assertions are syntax-checked but
**have not executed**. No screenshots or manual keyboard results were produced.
Socket restrictions cannot be lifted by this worker, so acceptance remains
partial until validation runs in a provisioned environment.

### Subsequent retry verification (2026-09-20, 18:39 UTC)

Rechecked commit `ad26cd1` in the assigned issue-4 branch. No application or
browser-script changes were warranted by the available evidence. This attempt
remains **PARTIAL**:

- `GOCACHE=/tmp/issue-4-go-cache go test ./...`: PASS; dashboard 0.004s;
  command package has no test files.
- `GOCACHE=/tmp/issue-4-go-cache go vet ./...`: PASS, exit 0.
- `GOCACHE=/tmp/issue-4-go-cache go build -o /tmp/issue-4-harness ./cmd/harness`:
  PASS, exit 0.
- `node --check scripts/check-dashboard.mjs` and
  `node --check internal/dashboard/assets/app.js`: PASS, exit 0.
- `git diff --check` and
  `git diff 3e9f1a6 --exit-code -- 'theme/Factory Floor.dc.html' theme/support.js`:
  PASS, exit 0; preserved references are unchanged.
- `/tmp/issue-4-harness`: FAIL, exit 1, at 18:39:26 UTC:
  `listen tcp 127.0.0.1:8080: socket: operation not permitted`.
- `node scripts/check-dashboard.mjs`: FAIL, exit 1:
  `ERR_MODULE_NOT_FOUND` for `playwright`. No `chromium`, `chromium-browser`,
  `google-chrome`, or `playwright` executable was found on PATH.

This worker's permission policy does not permit escalation. Completing the
remaining acceptance checks requires a provisioned validation environment with
loopback sockets, Playwright, Chromium, and the reference's React dependencies.
No screenshot diff, manual keyboard pass, browser reduced-motion check, or
offline network observation was completed in this attempt. The passing static
and handler checks do not substitute for those browser checks.

### Running the browser checks

Use an environment with Playwright and its Chromium already installed; these
are review tools, not dashboard dependencies. Serve the application above and
serve the repository reference using `python3 -m http.server 8081 --bind 127.0.0.1`.
Then run `node scripts/check-dashboard.mjs` from the repository root.

The script captures 1440×1000 and 390×844 screenshots of the served shell and the
original theme, writes pixel-diff images and changed-pixel fractions to
`/tmp/issue-4-screenshots`, and exercises tabs/filters by keyboard. It checks for
page errors, horizontal page overflow, active animations with reduced motion,
and nonlocal requests, then operates the shell with the browser offline.
The reference simulator is frozen, Google Fonts blocked for fallback comparison;
its React CDN resources must be available in the review environment. The shipped
shell has no such requirement. Diff metrics are diagnostic, not a claim of
pixel equality: Demo labels, fixture state and mobile stacking intentionally
differ. Inspect both image pairs for color, typography and panel fidelity.

Also perform a manual keyboard-only pass: Tab to the skip link, Tab to the tab
list, use Left/Right/Home/End to select views, Tab through the Events panel and
six filters, and activate filters with Space/Enter. Check visible focus, empty
filter feedback, and reverse traversal with Shift+Tab. Toggle reduced motion in
browser emulation. Disable external networking and reload the local application;
confirm the network panel contains only its three local resources. Record the
screenshots and manual outcomes before acceptance.

## Integration note

This independent task started without a Go module or command. It adds a minimal
standard-library module and CLI entrypoint solely to serve the assigned shell.
A concurrent Go foundation task may need to reconcile these two small files;
`internal/dashboard.Handler()` is the integration point.
