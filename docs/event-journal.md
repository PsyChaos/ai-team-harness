# Observation journal and HTTP API

`internal/journal` is an observation read model. It does not route or execute
work. GitHub remains authoritative. The temporary [Python observer bridge](python-observer-bridge.md)
produces real-source observations; `cmd/harness` still serves the fixture demo.

Create a journal with `journal.Open(path, journal.Limits{Retain: 1024,
Pending: 128, MaxEventBytes: 16384})`. The parent directory must already exist,
be private, and have exactly one owning Journal instance/process. Files are mode
0600. Storage is a version-one JSON checkpoint containing the retained events,
contiguous cursor and pending out-of-order records. Each accepted append writes
and syncs a temporary file, then atomically renames it before notifying readers.
Failed writes leave the visible cursor unchanged. Reopening restores both the
window and pending records. This supports process restart, not a guarantee against
power loss (the parent directory is not fsynced). A crash before rename can leave
an orphan `.journal-*` temporary file; clean these only while the writer is stopped.
Corrupt, oversized, incompatible or noncontiguous checkpoints fail to open.

## Event and ordering contract

An event has `id` (positive uint64), `type` (nonempty, at most 128 bytes), and `data`
(valid JSON). The **single logical producer** allocates immutable, consecutive IDs
starting at 1, and continues from persisted state after restart. It must replay
any missing IDs; multiple unordered producer ID spaces are not supported. Event
payloads are dashboard-safe observations, not credentials, logs containing secrets,
or signed broker actions. Payload filtering belongs to the source adapter.

IDs determine order, not arrival time. The first accepted payload for an ID wins;
replays at or below the committed cursor and duplicates in the pending buffer are
ignored. Later IDs wait for gaps to close. Missing IDs never time out or get skipped.
Pending-buffer overflow returns `ErrCapacity` without accepting the record; the
producer must retry. The next contiguous event is accepted even when the buffer
is full, allowing the gap to close. Events over `MaxEventBytes` (entire encoded
event) are rejected. Inputs and returned snapshots are copied to prevent mutation.
JSON IDs are integers: JavaScript clients must parse them losslessly or constrain
the producer to the JavaScript safe integer range. SSE cursor strings preserve
all uint64 digits. Sequence exhaustion requires a new journal/client resync.

## Read API

`Handler(HTTPOptions{MaxStreams: 32, WriteTimeout: 5*time.Second,
Heartbeat: 15*time.Second})` returns a mountable standard-library handler.
The embedding server owns authentication, connection limits, request timeouts,
and trusted/local listener configuration. No public listener is started here.

- `GET /snapshot`: JSON `{ "version": 1, "cursor": 42, "events": [...] }`.
  This is the retained observation window, not a full historical state projection.
  Replace the dashboard window with this snapshot, then stream after its cursor.
- `GET /events?cursor=42`: SSE events strictly after that cursor. An explicit
  `Last-Event-ID` header takes precedence. The default cursor is zero.
  Frames contain `id: 43`, `event: observation`, and `data: <event JSON>`.
  The transport sends an initial comment and periodic heartbeat comments.
- Invalid cursors return 400. Cursors ahead of the journal or older than
  `cursor - len(events)` return 409: fetch a new snapshot and reconnect.
- If an active stream falls behind retention, it receives an `event: reset` frame
  with `{"snapshot":"/snapshot"}` and closes. Fetch a snapshot and resume.
  If a write timeout prevents delivery of reset, reconnecting detects the stale
  cursor through 409. Snapshot/stream races can require another resync.

A client records the last **processed** event ID, reconnects with it, and ignores
IDs already processed if its own processing/cursor commit is interrupted. Resume
within retention has no gaps or duplicates at the API; this is not a transactional
exactly-once guarantee for client side effects.

## Retention and backpressure

Retention is count-based: only the newest `Retain` committed events and at most
`Pending` out-of-order records persist. No time-based retention is implied. Both
counts must be positive; event size must be 64 bytes through 1 MiB. Individual
counts are capped at 1 Mi entries and the configured checkpoint decoding bound
at 1 GiB. Limits must accommodate existing records when reopening.

There is no per-client event queue. Each stream holds at most one retained batch,
waits on a shared change signal, and releases its batch after writing. At most
`MaxStreams` SSE requests are admitted (additional requests return 503). Each
write/flush gets a `WriteTimeout`; deadline-unsupported transports return 500.
Slow clients disconnect instead of blocking ingestion or pinning journal retention.
The embedding server must also bound general HTTP connections/concurrent snapshot
requests. Journal memory/disk is O((Retain + Pending) * MaxEventBytes); admitted
stream memory is O(MaxStreams * Retain * MaxEventBytes), plus bounded encoding
copies. Atomic replacement temporarily uses two checkpoints. Writes rewrite the
checkpoint, trading throughput for a simple bounded, dependency-free contract.

Run `go test ./internal/journal/...` and `go test -race ./...`. Tests cover persisted
pending events/restart, deduplication, reordering, failed writes, malformed records,
retention under 1,000 appends, HTTP snapshot and disconnect/reconnect, cursor errors,
stream admission and slow-writer deadlines under sustained ingestion. HTTP tests
use real HTTP client/server encoding over `net.Pipe`, without requiring a TCP bind.
