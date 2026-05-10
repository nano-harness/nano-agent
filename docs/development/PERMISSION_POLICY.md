# Permission Policy

This document records the refactor-era permission and approval model.

## Permission sources

Permissions are derived from:

- global config and CLI flags;
- tool metadata and tool categories;
- command frontmatter such as `allowed-tools` and `permission-profile`;
- AgentProfile fields such as `permission_mode` and `allowed_tools`;
- hook decisions;
- sandbox path/network/process checks;
- daemon or TUI user approval decisions.

More specific scopes should narrow broader scopes rather than expanding them silently.

## Decision flow

1. Normalize the requested tool action and parameters.
2. Apply command or teammate scoped tool restrictions.
3. Run static security checks and configured allow/deny rules.
4. Run hooks through `pkg/hookservice`.
5. Ask for user approval when required.
6. Record the final decision in audit JSONL when audit middleware is enabled.

The decision actions are:

- `allow`: execute immediately.
- `confirm`: wait for explicit user approval.
- `block`: refuse execution.

## Scoped tool restrictions

Slash commands may declare:

```yaml
allowed-tools: [run_shell_command]
permission-profile: acceptEdits
```

Agent profiles may declare:

```yaml
permission_mode: acceptEdits
allowed_tools: [read_file, run_shell_command]
```

For teammates, `permission_mode` and `allowed_tools` are applied independently of the parent agent. A teammate profile must not broaden the parent process by mutating shared config.

## Approval compatibility

Daemon team sessions support these approval frames:

- `tool_approval`
- `approve`
- `reject`

The approval request is also visible as a `waiting_for_user` event with `metadata.kind=tool_approval_request`.

## Audit fields

Audit entries include:

- schema version;
- tool name;
- sanitized parameters;
- success/error status;
- duration;
- optional `security_decision`.

Sensitive keys such as tokens, passwords, secrets, and API keys are redacted before writing audit JSONL.

