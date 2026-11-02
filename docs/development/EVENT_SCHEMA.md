# Event Schema

[中文](./EVENT_SCHEMA.zh-CN.md)

This document summarizes public event, replay, and audit schema contracts. Field-level examples are maintained in `docs/EXTENSION_EVENT_SCHEMA.md`.

## Public stream events

Public event consumers should use stable stream event fields:

- `type`
- `session_id`
- `run_id`
- `seq`
- `metadata`
- `payload`

CLI, TUI, and daemon clients should consume the same public event shape rather than private agent state.

## Replay

Replay uses sequence numbers:

- REST team replay: `GET /api/v1/teams/sessions/{id}/events?since_seq=N`
- WebSocket live subscription: `subscribe`
- WebSocket replay-only request: `replay`

Clients should persist the last processed `seq` and resume from that sequence after reconnecting.

## Approval events

Tool approval requests are represented as `waiting_for_user` events with:

```json
{
  "kind": "tool_approval_request"
}
```

Daemon clients may respond with `tool_approval`, `approve`, or `reject` frames.

## Audit JSONL

Audit entries include `schema_version`, sanitized tool parameters, execution result, duration, and optional security decision fields. Audit logs must redact sensitive values before persistence.

