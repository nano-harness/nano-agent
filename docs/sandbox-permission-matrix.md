# Sandbox × Permission Mode Matrix

> Expected behavior of `nano binary exec` across all sandbox/permission combinations.
> This matrix serves as the single source of truth for both nano-agent tests and
> symphony's `resolveSandboxAndPermission` validation.

---

## Matrix

| Platform | `--sandbox` | Permission Mode    | Expected Behavior                                              | Status  |
|----------|-------------|--------------------|----------------------------------------------------------------|---------|
| darwin   | `on`        | `default`          | sandbox-exec active; prompts for dangerous tools               | ✅ pass |
| darwin   | `on`        | `acceptEdits`      | sandbox-exec active; file edits auto-approved                  | ✅ pass |
| darwin   | `on`        | `auto`             | sandbox-exec active; LLM classifier decides approval           | ✅ pass |
| darwin   | `on`        | `yolo`             | sandbox-exec active; all tools auto-approved                   | ✅ pass |
| darwin   | `on`        | `plan`             | sandbox-exec active; read-only (no mutations allowed)          | ✅ pass |
| darwin   | `off`       | `default`          | no sandbox; prompts for dangerous tools                        | ✅ pass |
| darwin   | `off`       | `acceptEdits`      | no sandbox; file edits auto-approved                           | ✅ pass |
| darwin   | `off`       | `auto`             | no sandbox; LLM classifier decides approval                   | ✅ pass |
| darwin   | `off`       | `yolo`             | no sandbox; all tools auto-approved                            | ✅ pass |
| darwin   | `off`       | `plan`             | no sandbox; read-only (no mutations allowed)                   | ✅ pass |
| darwin   | `auto`      | `default`          | sandbox only if embedded*; prompts for dangerous tools         | ✅ pass |
| darwin   | `auto`      | `yolo`             | sandbox only if embedded*; all tools auto-approved             | ✅ pass |
| linux    | `on`        | `default`          | native sandbox (seccomp/namespace); prompts for dangerous tools| ✅ pass |
| linux    | `on`        | `acceptEdits`      | native sandbox; file edits auto-approved                       | ✅ pass |
| linux    | `on`        | `auto`             | native sandbox; LLM classifier decides approval               | ✅ pass |
| linux    | `on`        | `yolo`             | native sandbox; all tools auto-approved                        | ✅ pass |
| linux    | `on`        | `plan`             | native sandbox; read-only (no mutations allowed)               | ✅ pass |
| linux    | `off`       | `default`          | no sandbox; prompts for dangerous tools                        | ✅ pass |
| linux    | `off`       | `acceptEdits`      | no sandbox; file edits auto-approved                           | ✅ pass |
| linux    | `off`       | `auto`             | no sandbox; LLM classifier decides approval                   | ✅ pass |
| linux    | `off`       | `yolo`             | no sandbox; all tools auto-approved                            | ✅ pass |
| linux    | `off`       | `plan`             | no sandbox; read-only (no mutations allowed)                   | ✅ pass |
| linux    | `auto`      | `default`          | sandbox only if embedded*; prompts for dangerous tools         | ✅ pass |
| linux    | `auto`      | `yolo`             | sandbox only if embedded*; all tools auto-approved             | ✅ pass |

*"embedded" (orchestrator-spawned) = `SYMPHONY_WORKSPACE`, `SYMPHONY_MCP_URL`, or `NANO_ORCHESTRATOR_PROFILE` env set.

---

## Invariants (All Combinations)

For **every** cell in the matrix, the following must hold:

1. **Process exits cleanly** — exit code is one of `{0, 1, 10, 20, 30}`.
2. **stdout last line is valid JSON** — parseable as `binaryResultSummary`.
3. **`<output-dir>/result.json` exists** — byte-identical to stdout JSON.
4. **`<output-dir>/solution.patch`** — either absent (no changes) or a valid unified diff.
5. **Permission mode is honored** — `plan` mode produces no filesystem mutations; `yolo` produces no permission prompts.

---

## Permission Mode Semantics

| Mode          | File Edits | Shell Commands | Dangerous Ops | Notes                              |
|---------------|------------|----------------|---------------|------------------------------------|
| `default`     | Prompt     | Prompt         | Prompt        | Interactive confirmation required  |
| `acceptEdits` | Auto       | Prompt         | Prompt        | Only file write tools auto-approved|
| `auto`        | Classify   | Classify       | Classify      | LLM classifier risk assessment     |
| `yolo`        | Auto       | Auto           | Auto          | Everything auto-approved           |
| `plan`        | Deny       | Deny           | Deny          | Read-only; no mutations            |

---

## Sandbox Backend Behavior

| Backend   | Platform        | Mechanism                              |
|-----------|-----------------|----------------------------------------|
| `native`  | darwin          | `sandbox-exec` with custom profile     |
| `native`  | linux           | seccomp + mount namespaces             |
| `docker`  | darwin / linux  | One-shot container isolation           |
| (none)    | any             | No isolation (sandbox disabled)        |

---

## Side Effects of Mode Combinations

| Combination                           | Side Effect                                    |
|---------------------------------------|------------------------------------------------|
| `yolo` + no sandbox backend configured | Auto-sets `sandbox.backend = "docker"`        |
| `auto` + no `permission_auto` config  | Warning emitted; falls back to `default` behavior |
| `auto` + `ConfirmPolicy=allow`        | Overridden to `block` for fail-closed          |

---

## Environment Variable Interaction

When both `--sandbox` flag and `NANO_SANDBOX_*` env vars are present:

```
--sandbox=off  →  sandbox disabled (env vars ignored for enabled state)
--sandbox=on   →  sandbox enabled; NANO_SANDBOX_BACKEND/NETWORK_ACCESS still apply for sub-settings
--sandbox=auto →  sandbox enabled only if embedded; env vars apply for sub-settings
```

The `NANO_SANDBOX_ENABLED` env var is applied at config load time (`pkg/config`),
but `applyBinarySandboxMode()` can override the `Enabled` field afterward. This
means `--sandbox=off` **always wins** over `NANO_SANDBOX_ENABLED=true`.

---

## CI Validation

To validate this matrix in CI:

```bash
# Minimal smoke test: verify stdout JSON and exit code
nano binary exec --output-dir=/tmp/test-out --sandbox=off --permission-mode=yolo "echo hello"
jq . /tmp/test-out/result.json  # must succeed (valid JSON)

# Verify solution.patch format (if exists)
if [ -f /tmp/test-out/solution.patch ]; then
  # Must be valid unified diff or empty
  head -1 /tmp/test-out/solution.patch | grep -qE '^(diff |---|\+\+\+|$)'
fi
```

A full matrix CI run should iterate over `{on,off} × {default,acceptEdits,auto,yolo,plan}`
on both darwin and linux runners.
