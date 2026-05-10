# Ralph-loop

Ralph-loop lets a Stop hook request another agent turn by returning a block decision, for example:

```json
{"decision":"block","reason":"Continue with the remaining checklist"}
```

When enabled, nano-agent treats the hook reason as the next user input and starts a new turn with the previous conversation history preserved.

## Configuration

```yaml
hooks:
  ralph:
    enabled: true
    max_iterations: 10
    hard_max_iterations: 50 # values above 50 are clamped to the built-in safety cap
```

Set `hooks.ralph.enabled: false` to keep Stop hook block decisions from restarting turns.

## Stop hook payload

Stop hooks receive Claude Code-compatible fields in `NANO_HOOK_INPUT`:

- `hook_event_name`
- `session_id`
- `transcript_path`
- `cwd`
- `stop_hook_active`
- `iteration`

`stop_hook_active=true` means the current turn was already started by ralph-loop. Hooks should not return another block in that state; nano-agent also ignores such blocks to prevent recursion.

## Transcript

Each session appends JSONL records to:

```text
~/.nano-agent/sessions/<session_id>/transcript.jsonl
```

Long sessions can grow large. Deleting a session removes its transcript directory.

## Advanced hook fields

Hook output supports:

- `decision`: `block`, `approve`, or `continue`; this takes priority over `action`
- `systemMessage`: status text surfaced as a warning/status event
- `continue: false`: explicitly allows the turn to finish
- `suppressOutput`: recorded as hook metadata

Hook config supports:

- `once: true`: run a hook only once per service lifetime
- `async: true`: run a command hook in the background
- `async_rewake: true`: background command exit code `2` sends a mailbox wakeup message
- `status_message`: static status text metadata for hook integrations
