# nano CLI Commands & 三端能力一致性矩阵

This document tracks the public, scriptable surface exposed by the `nano`
binary, and the parity matrix between the three primary frontends:

- **TUI (BubbleTea)** — interactive single-agent / team-lead REPL.
- **TUI (TView)** — alternative interactive frontend.
- **Daemon** — long-running HTTP/WebSocket service.
- **Binary** — non-interactive one-shot commands (`nano binary ...`)
  intended for CI, scripts, and benchmark drivers.

## Top-level commands

| Command                        | Description                                                       |
| ------------------------------ | ----------------------------------------------------------------- |
| `nano` (no args)               | Default interactive single-agent REPL                             |
| `nano chat --team <name>`      | Long-running team-lead REPL with mailbox                          |
| `nano lead-chat`               | Lead chat (team coordinator) REPL                                 |
| `nano teammate ...`            | Internal-use spawner for subprocess teammates                     |
| `nano daemon ...`              | Daemon lifecycle: start/stop/status/logs                          |
| `nano session ...`             | Session lifecycle: list/load/prune                                |
| `nano model[s] ...`            | Model listing, switching, and reasoning controls                  |
| `nano routines ...`            | Cron routine registration                                         |
| `nano events` / `nano audit`   | Read-only views into daemon event/audit streams                   |
| `nano doctor`                  | Health check                                                      |
| `nano mcp ...`                 | MCP server management (status/test/start/stop/restart)            |
| `nano binary ...`              | Non-interactive one-shot operations (see below)                   |

## `nano binary` subcommands

Metadata/list subcommands accept `--json` for machine-readable output.
`binary swebench` preserves SWE-bench patch-output compatibility.

| Subcommand               | Purpose                                                  |
| ------------------------ | -------------------------------------------------------- |
| `binary query <prompt>`  | Run a single prompt and exit (replaces `--binary-mode`)  |
| `binary list-models`     | List configured provider/model presets                   |
| `binary list-tools`      | List built-in tool descriptors                           |
| `binary list-slash`      | List built-in slash commands                             |
| `binary list-skills`     | List installed skills (personal + project)               |
| `binary swebench <p>`    | SWE-bench-compatible one-shot evaluation                 |

The legacy `--binary-mode` global flag is still accepted but emits a
deprecation warning recommending `nano binary swebench`. It will be removed
in a future release.

## 三端能力一致性矩阵

The following matrix tracks whether each user-facing capability is reachable
from each frontend. ✅ = supported, ⚠️ = partial / wrapped, ❌ = not yet wired.

| Capability                              | BubbleTea | TView | Daemon | Binary |
| --------------------------------------- | :-------: | :---: | :----: | :----: |
| One-shot prompt → answer                |    ✅    |  ✅  |   ✅   |   ✅   |
| Permission mode switching (`/yolo`, `/permission`, `/plan`) | ✅ | ✅ | ✅ | n/a |
| Session allowlist (`/allow`, `/disallow`, `/permissions`)   | ✅ | ✅ | ✅ | n/a |
| List teammates (`/agents`, `/teammates`) | ✅ | ✅ | ✅ | ✅ (`binary list-tools` lists agent-spawn tools) |
| Show teammate detail                    |    ✅    |  ✅  |   ✅   |  ❌   |
| Checkpoints (`/checkpoint`, `/restore`) | ✅ when `checkpoint.enabled` | ✅ when `checkpoint.enabled` | ❌ | ❌ |
| Models listing (`/models`, `binary list-models`) | ✅ | ✅ | ✅ | ✅   |
| Tools listing                           |    ❌    |  ❌  |   ✅   |   ✅   |
| Slash command listing                   |    ✅    |  ✅  |   ✅   |   ✅   |
| Skills listing (`/skill:list`, `binary list-skills`) | ✅ | ✅ | ✅ | ✅   |
| Routines listing                        |    ✅    |  ✅  |   ✅   |  ❌   |
| OpenSpec workflow (`/opsx:*`)           |    ✅    |  ✅  |   ✅   |  ❌   |
| `@file` reference expansion             |    ✅    |  ❌  |   ❌   |  n/a   |
| Reverse history search (Ctrl+R)         |    ✅    |  ❌  |   n/a  |  n/a   |
| Daemon log tail (`daemon logs --follow`) |   n/a   | n/a  |   ✅   |   ✅   |
| MCP server probe / disable              |   n/a    | n/a  |   ✅   |   ✅   |
| Session prune (`session prune --dry-run`) | n/a    | n/a  |   ✅   |   ✅   |

Checkpoint slash commands are backed by `pkg/checkpoint` in BubbleTea/TView
when `checkpoint.enabled: true`. When disabled, the command is still
recognized and returns a friendly "not enabled" message instead of being
silently ignored.

## Migration notes

- `--binary-mode` → `nano binary swebench` (or `nano binary query`).
  See `swe_bench_test/run_swe_bench.py` for the updated invocation.
- `nano mcp servers stop <name>` now persistently disables the server in
  the active config rather than pretending to "stop a process". Use
  `nano mcp servers start <name>` to re-probe and re-enable.
