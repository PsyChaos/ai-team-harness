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
