# Shell Output Capture & Inline Budget

[中文](./SHELL_OUTPUT.zh-CN.md)

`run_shell_command` applies two independent output limits: an absolute **capture limit** and a much smaller **inline budget** for what actually enters the model context. Oversized output is spilled to a file (spill-to-file), so a runaway command can never blow up the conversation context.

## Overview

- **Capture limit** (`shell.capture_output_max_bytes`, default 16MB): the absolute maximum of stdout/stderr kept per stream while a command runs. Beyond this, the oldest bytes are dropped and the result is marked `output_truncated`.
- **Inline budget** (`shell.inline_output_max_bytes`, default 64KB): the maximum combined stdout+stderr inlined into the model context. When captured output exceeds the budget, the full output is written to a spill file and the context receives only a head/tail preview plus the file path.

This mirrors the 2026 industry practice (e.g. Claude Code's `bashOutputMaxChars`): a small, stable per-tool-output budget significantly reduces per-turn token cost while keeping full output recoverable on disk.

## Configuration

```yaml
shell:
  inline_output_max_bytes: 65536     # 64KB default; <= 0 falls back to default
  capture_output_max_bytes: 16777216 # 16MB default; <= 0 falls back to default
```

## Spill-to-file behavior

When captured output exceeds the inline budget:

1. The full captured output is written to `<user-cache-dir>/nano-shell-output/shell-output-<timestamp>-<pid>.txt` (mode `0600`; falls back to the OS temp dir when the user cache dir is unavailable).
2. The inline content becomes a **head preview (~80% of the budget)** and a **tail preview (~20%)**, split proportionally between stdout and stderr, with an omission marker (`... [N bytes omitted] ...`).
3. A hint line is appended: `[output truncated: full output written to <path>, N bytes total]`.

The tool result metadata includes:

| Key | Meaning |
| --- | --- |
| `output_spilled` | `true` when the output was spilled to a file |
| `output_file` | Absolute path of the spill file |
| `total_bytes` | Total captured bytes (stdout + stderr) before preview |
| `inline_output_max_bytes` | The inline budget in effect |
| `output_truncated` / `max_output_bytes` | Unchanged: set when the 16MB-class capture limit was hit |

Streaming output callbacks and background tasks (`is_background`, auto-background on timeout) are unaffected: the inline budget applies only to the foreground result returned to the model.
