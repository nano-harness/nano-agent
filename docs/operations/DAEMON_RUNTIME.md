# Daemon Runtime

The daemon exposes HTTP and WebSocket APIs over public session and event state.

## Session streams

Team-lead sessions stream over:

```text
GET /api/v1/teams/sessions/{id}/stream
```

Supported WebSocket frame types include:

- `subscribe`
- `replay`
- `lead_input`
- `cancel`
- `tool_approval`
- `approve`
- `reject`
- `ping`

## Replay API

Historical team session events are exposed through:

```text
GET /api/v1/teams/sessions/{id}/events?since_seq=N
```

The daemon returns sequenced events from the session event store.

## Approval flow

When the lead agent needs approval, it emits a `waiting_for_user` event with `metadata.kind=tool_approval_request`. Clients can approve or reject using the WebSocket approval frames.

## Decoupling rule

Daemon and UI code should consume public session/event state. They should not rely on private agent turn internals for rendering, replay, approval, or cancellation.

## Compatibility

Existing daemon endpoints and frame names remain supported unless explicitly documented as deprecated in [Daemon API](DAEMON_API.md).

## Log streaming

The daemon writes structured logs to the file referenced by `daemon.log_file`
(default: `~/.nano/daemon.log`). The CLI provides two read modes:

- `nano daemon logs -n <N>` prints the trailing `N` lines once.
- `nano daemon logs --follow` (alias `-f`) streams new content as it is
  appended, similar to `tail -f`. The follow implementation polls the file
  every 200 ms and detects logrotate-style rotation by comparing the inode of
  the current file (Unix) or its size+mtime (other platforms). When rotation
  is observed, the tailer transparently re-opens the file and resumes from
  the start of the new file.

Cancellation: `Ctrl+C` (SIGINT) cancels the follow context; `tailFollow`
returns once the context is done so the CLI exits cleanly without leaking
file handles.

## Scribe & OSS sync

Session events are persisted by `SessionScribe`
([`pkg/daemon/session_scribe.go`](../../pkg/daemon/session_scribe.go)) which
appends JSON-Lines records to `~/.nano/sessions/<id>/events.jsonl` (or
`/tmp/nano-agent/sessions/<id>/events.jsonl` on macOS).

Behavior:

- Each scribe owns a buffered writer guarded by a mutex; writes are committed
  by a debounced sync timer so high-frequency events are batched without
  blocking the producer.
- When `daemon.scribe.oss_*` is configured, the scribe also uploads completed
  rotations to the configured OSS bucket using
  `aliyun-oss-go-sdk`. Uploads are best-effort and run asynchronously; failures
  are logged but never block local persistence.
- `Close()` flushes pending content and triggers a final upload before
  returning.

## Scheduler

The scheduler is wired by `Server` and exposed at `/api/v1/scheduler/*`
(see [`pkg/daemon/scheduler_handlers.go`](../../pkg/daemon/scheduler_handlers.go)).
Implementation notes:

- Scheduling uses `github.com/robfig/cron/v3` for cron expression parsing
  and dispatch.
- Tasks can be added, listed, paused, resumed, and removed at runtime via
  the HTTP API; the in-memory cron table is kept in sync with the persistent
  routine store under `~/.nano/cron/`.
- On daemon restart the scheduler reloads persisted routines from the same
  store so previously registered jobs keep firing without manual
  re-registration.

## Connection Manager

`ConnectionManager`
([`pkg/daemon/connection_manager.go`](../../pkg/daemon/connection_manager.go))
owns each WebSocket connection and applies gorilla/websocket best practices:

- All writes are funneled through a single goroutine that consumes a
  `WriteMessage` channel; this is required because gorilla/websocket does
  not allow concurrent writers.
- A heartbeat goroutine emits ping frames at the configured interval and
  enforces read deadlines so half-open connections are detected.
- Outbound payloads larger than `MaxMessageSize` (64 KB) are split into
  `ChunkSize` (32 KB) chunks and re-assembled by clients via the
  `ChunkedMessage` envelope (`id`, `index`, `total`, `complete`).


