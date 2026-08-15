# Sandbox Design

[中文](./SANDBOX_DESIGN.zh-CN.md)

This document supplements the sandbox design direction for nano-agent. The current implementation already has basic sandbox capabilities: on Linux, Shell commands are wrapped with `bwrap`; on macOS, Shell commands are wrapped with `sandbox-exec`; the in-process Go file tools enforce path-level access control through `PathChecker`; and configuration options such as `AllowedPaths`, `BlockedPaths`, `ReadOnlyPaths`, `ExtraReadOnlyPaths`, and `ExtraWritablePaths` are supported. The subsequent design goal is to upgrade these capabilities from "Shell command wrappers" into a unified Sandbox Runtime, and to make Docker the higher-priority, stronger-isolation execution backend.

## 1. Design Goals

The nano-agent sandbox should not only answer "is a command allowed to execute"; it also needs to address the following problems:

- Limit the scope of the agent's access to the host filesystem.
- Limit the side effects of Shell commands, Hooks, MCP Servers, and sub-agents.
- Isolate networking, environment variables, processes, temporary files, and credentials.
- Provide a safety net for high-privilege modes such as YOLO/acceptEdits.
- Support auditable, rollbackable, and reproducible task execution environments.
- Support different scenarios such as local development, Daemon, CI, SWE-bench, and team mode.

The core principle is:

> The permission system decides "whether something can be done"; the sandbox system decides "even if it is done, it can only affect a controlled environment".

## 2. Sandbox Layering Model

A three-layer sandbox model is recommended.

### 2.1 Policy Sandbox

The Policy Sandbox is responsible for "whether execution is allowed". It does not provide real process isolation; it makes policy decisions.

Applies to:

- `run_shell_command`
- File tools
- Web tools
- MCP tools
- Hook
- Slash Command prelude
- Subagent tool calls

Capability scope:

- Permission mode evaluation.
- allow / deny rules.
- Session allowlist.
- CommandGuard.
- Hook decisions.
- User confirmation.
- Audit logging.

### 2.2 Path Sandbox

The Path Sandbox is responsible for "which host paths the agent can access", and continues to evolve from the current `PathChecker`.

Applies to:

- `read_file`
- `write_file`
- `edit_file`
- `delete_file`
- workspace tools
- File operations executed inside the Go process

Capability scope:

- Paths inside the working directory are accessible by default.
- Sensitive paths are denied by default.
- Configurable read-only paths.
- Configurable additional writable paths.
- Resolve symlinks to prevent path traversal.
- For new files that do not exist yet, resolve the nearest existing parent directory.

### 2.3 Execution Sandbox

The Execution Sandbox is responsible for "actually isolating child processes".

Applies to:

- Shell commands
- Hook scripts
- MCP stdio servers
- Slash Command prelude
- Long-running tasks such as builds, tests, and dependency installation
- Subagent execution environments

Recommended backends:

- `none`
- `native`
  - Linux: `bwrap`
  - macOS: `sandbox-exec`
- `docker`

Future optional backends:

- `podman`
- `firecracker`
- `nsjail`

Docker should be the recommended backend, especially for YOLO, Daemon, CI, SWE-bench, and remote task scenarios.

## 3. Docker Sandbox Design

### 3.1 Why Docker Is the Better Choice

Compared with `bwrap` / `sandbox-exec`, Docker's advantages are:

- Better cross-platform consistency.
- Clearer isolation boundaries.
- Can limit CPU, memory, process count, networking, and filesystem.
- Well suited for long-running tasks, CI, Daemon, and SWE-bench.
- Can build reproducible development environments.
- Easier snapshotting, caching, cleanup, and auditing.
- Safer for YOLO mode: even if dangerous commands are executed automatically, they are confined inside the container.
- Can isolate the agent's execution environment from the user's host, reducing the risk of accidental deletion, credential leakage, and pollution of the system environment.

Recommended backend priority:

1. An explicitly configured backend.
2. Prefer Docker for Daemon / CI / YOLO scenarios.
3. Use native for lightweight local interaction.
4. Fall back to none only when no backend is available, with a prominent warning.

### 3.2 Docker Execution Modes

The Docker backend should support three execution modes.

#### One-off Container Mode

Each Shell command starts a temporary container.

Advantages:

- Strongest isolation.
- Every command runs cleanly.
- Easy to clean up.

Disadvantages:

- Higher startup overhead.
- Unfriendly to dependency installation and build caches.

Suitable for:

- High-risk commands.
- Hook.
- Untrusted MCP.
- Dangerous commands in YOLO mode.
- CI audit tasks.

#### Session-level Persistent Container

Each Agent session creates one persistent container, and all commands run inside the same container.

Advantages:

- Better performance.
- Preserves dependency installation results.
- Suitable for interactive development.
- Suitable for long tasks and multi-round modifications.

Disadvantages:

- Requires lifecycle management.
- State inside the container may accumulate pollution.

Suitable for:

- TUI / Daemon long sessions.
- Multi-round coding tasks.
- Build, test, and fix loops.
- SWE-bench.

#### Task-level Isolated Container

Each Turn or each Task creates one container.

Advantages:

- More efficient than one-off commands.
- More controllable than session-level containers.
- Convenient for task-level rollback and auditing.

Suitable for:

- Subagent.
- Team mode.
- Batch tasks.
- OpenSpec implementation task.

Recommended defaults:

- Local normal mode: session-level container.
- YOLO mode: task-level or one-off container.
- Daemon mode: session-level container + strict resource limits.
- CI mode: one-off or task-level container.

## 4. Sandbox Runtime Abstraction

A unified abstraction is recommended so that different execution scenarios do not depend directly on specific backends.

### 4.1 SandboxRuntime

`SandboxRuntime` is responsible for:

- Creating execution environments.
- Wrapping commands.
- Mounting the workspace.
- Injecting environment variables.
- Controlling networking.
- Setting resource limits.
- Collecting logs.
- Cleaning up containers or temporary directories.

Current implementation status:

- `pkg/sandbox` already defines `Runtime`, `SandboxRequest`, `SandboxEnvironment`, `SandboxResult`, `Backend`, `NetworkPolicy`, `Mount`, and `ResourceLimits`.
- `NewRuntime` wires the existing `none`, Linux `bwrap`, and macOS `sandbox-exec` backends into a unified adapter without changing existing execution behavior.
- `NewRuntime` already supports the one-off container mode of an explicit `docker` backend, configured via `sandbox.backend`, `sandbox.docker_image`, `NANO_SANDBOX_BACKEND`, and `NANO_SANDBOX_DOCKER_IMAGE`.
- When the CLI permission mode is `yolo` and no sandbox backend is explicitly configured, the Docker backend is enabled by default; if a more reproducible container image is needed, `sandbox.docker_image` can be configured in digest form.
- `run_shell_command` already wraps the actual command through `SandboxRuntime.PrepareCommand`, writes backend, network, mount, resource limit, and fallback information into the tool result metadata, and publishes sandbox command lifecycle audit events.
- `hookservice` can route hook shell commands into the unified sandbox entry via `Options.SandboxRuntime`.
- MCP stdio server startup commands are wrapped via the `SandboxRuntime` injected by Toolbox.
- Slash Command `!prelude` commands can enter the unified sandbox entry via `slash.CommandRuntime`.
- Team/Subagent metadata already records sandbox policy, and it is used for session-level/task-level Docker container lifecycle management.
- `SandboxRuntime` can publish sandbox audit events, and the Daemon `TaskEventStore` can already persist and replay sandbox events.
- In-process Go file tools continue to use `PathChecker` for path-level sandbox checks.

### 4.2 SandboxSession

`SandboxSession` represents the sandbox instance of one Agent session, and is responsible for storing:

- session id.
- container id.
- workspace mount.
- network policy.
- resource configuration.
- lifecycle state.

Sessions may be short-lived or reused.

### 4.3 SandboxExecutor

`SandboxExecutor` is responsible for executing a single command inside the sandbox, and needs to support:

- stdout / stderr streaming output.
- Cancellation.
- Timeout.
- Background tasks.
- exit code.
- Audit records.

## 5. Docker Mount Policy

Default mounts:

- Working directory: mounted read-write at `/workspace`.
- Temporary directory: an independent `/tmp` inside the container.
- User home: not mounted by default.
- `.git`: mounted by default, to support git diff / status.
- SSH, GPG, AWS, Kube, Docker socket: not mounted by default.
- Docker socket: denied by default unless explicitly enabled.

Suggested mount rules:

- `workspace`: read-write.
- `extra_read_only_paths`: read-only.
- `extra_writable_paths`: read-write.
- `blocked_paths`: not mounted.
- `secrets`: injected only through explicit secret mounts or an environment variable allowlist.

Strongly recommended to deny mounting by default:

- `~/.ssh`
- `~/.gnupg`
- `~/.aws`
- `~/.kube`
- `~/.docker`
- `/var/run/docker.sock`
- `/etc`
- `/root`
- The user's entire home directory

## 6. Docker Network Policy

Network policy should be configured according to task risk.

Suggested support:

- `network: none`
  - No network at all.
  - Suitable for code analysis, testing, and formatting.
- `network: restricted`
  - Only allow allowlisted domains or a proxy from configuration.
  - Suitable for dependency downloads.
- `network: bridge`
  - The default Docker network.
  - Suitable for ordinary development tasks, but requires auditing.
- `network: host`
  - Not recommended by default.
  - Only enabled explicitly by developers.

Recommended defaults:

- read-only / analysis: `none`.
- build / test: `none` or `restricted`.
- dependency install: `restricted`.
- web / MCP: opened individually per tool permission.
- YOLO: `none` by default; explicit confirmation required when networking is needed.

## 7. Docker Resource Limits

Resource limits must be supported to prevent agent-generated commands from overwhelming the host.

Suggested configuration items:

- CPU limit.
- Memory limit.
- pids limit.
- Disk write cap.
- Per-command timeout.
- Maximum container lifetime.
- Maximum number of background tasks.
- Maximum output size.
- Maximum number of files.
- Maximum network traffic, optional later.

Recommended defaults:

- CPU: 2 cores.
- Memory: 2GB or 4GB.
- PIDs: 256.
- Command timeout: 120 seconds.
- Long tasks automatically moved to background.
- Output limit follows the current shell output limit.

## 8. Docker Image Policy

### 8.1 Default Base Images

Officially recommended images can be provided, for example:

- `nano-agent/sandbox:go`
- `nano-agent/sandbox:node`
- `nano-agent/sandbox:python`
- `nano-agent/sandbox:full`

### 8.2 Project Custom Images

A project can configure in `.nano.yaml`:

- image.
- dockerfile.
- build context.
- build args.
- init command.

### 8.3 Automatic Image Detection

An image can be suggested automatically based on project files:

- `go.mod` → Go image.
- `package.json` → Node image.
- `pyproject.toml` / `requirements.txt` → Python image.
- `Cargo.toml` → Rust image.
- `pom.xml` → Maven image.

Automatic detection is only a suggestion; unknown images should not be pulled silently.

### 8.4 Image Security

- Do not automatically use untrusted images.
- Prompt for the source on first use of an image.
- Trusted registries can be configured.
- Image digests can be recorded.
- Pinning digests is recommended in Daemon mode.

## 9. Configuration Design

It is recommended to extend the `sandbox` configuration. The following fields are design goals and are not required to be implemented all at once:

```yaml
sandbox:
  enabled: true
  backend: docker        # none | native | docker
  mode: session          # command | task | session
  network: none          # none | restricted | bridge | host
  image: nano-agent/sandbox:go
  workdir: /workspace

  mounts:
    workspace:
      host_path: "."
      container_path: "/workspace"
      mode: rw
    extra_read_only:
      - "/usr/include"
    extra_writable: []

  blocked_paths:
    - "/etc"
    - "/root"
    - "~/.ssh"
    - "~/.aws"
    - "~/.kube"
    - "~/.docker"

  env:
    allow:
      - "PATH"
      - "HOME"
      - "LANG"
      - "GOPROXY"
      - "NPM_CONFIG_REGISTRY"
    block:
      - "NANO_API_KEY"
      - "OPENAI_API_KEY"
      - "ANTHROPIC_API_KEY"
      - "AWS_SECRET_ACCESS_KEY"

  docker:
    socket: ""           # default disabled
    pull_policy: missing # never | missing | always
    auto_remove: true
    reuse_session: true
    user: "1000:1000"
    read_only_rootfs: false
    cap_drop:
      - "ALL"
    security_opt:
      - "no-new-privileges:true"
    memory: "4g"
    cpus: "2"
    pids_limit: 256
```

## 10. Relationship with the Permission System

The Docker sandbox is not a replacement for the permission system; it is its safety net.

Recommended rules:

- `default` mode:
  - Dangerous commands still require confirmation.
  - After confirmation, they run inside the sandbox.
- `acceptEdits`:
  - File edits are allowed, but only take effect on the mounted workspace.
- `yolo`:
  - Executes automatically, but a Docker or native sandbox must be forcibly enabled.
  - If no sandbox backend is available, entering YOLO should be blocked or require a strongly warned second confirmation.
- `daemon`:
  - Requires the Docker backend by default.
  - Non-Docker backends require explicit configuration.
- `ci`:
  - Docker backend enabled by default.
  - Network disabled by default.

## 11. Relationship with Hooks

Hooks must also run inside the sandbox and must not bypass tool permissions.

Suggestions:

- PreToolUse / PostToolUse Hooks execute in a Docker or native sandbox by default.
- Hooks only receive structured JSON input.
- Hooks do not inherit secrets by default.
- Hook timeouts are shorter than ordinary shell commands.
- The Hook failure policy can be configured as `allow`, `confirm`, or `block`.
- Hook stdout / stderr goes into the audit log.
- Hooks are not allowed to access the Docker socket by default.

## 12. Relationship with MCP

MCP Servers are high-risk extensions and should be included in the sandbox.

Suggestions:

- stdio MCP servers can run inside a Docker sandbox.
- Each MCP server can be configured with an independent backend, image, mounts, and network.
- MCP servers are not allowed to access the host home directory by default.
- MCP server permissions must go into the Extension Manifest.
- Untrusted MCP servers default to read-only, with no network or a restricted network.
- MCP tool calls must also go through the Policy Pipeline.

## 13. Relationship with Multi-Agent / Swarm

Different agents can have different sandboxes.

Suggestions:

- Team Lead uses the main session sandbox.
- Subagent creates a task-level sandbox by default.
- Investigator uses a read-only sandbox.
- Coder uses a workspace rw sandbox.
- Researcher can use a network-restricted sandbox.
- Untrusted Agent uses a one-off Docker container.
- Each agent's sandbox id, container id, mounts, and network policy go into the event stream and audit log.

## 14. Relationship with Background Tasks

The current shell supports background tasks; the Docker backend needs to define the long-task lifecycle clearly.

Suggestions:

- Background tasks are bound to a `SandboxSession`.
- If the session container exits, background tasks are cancelled automatically.
- `bash_output` reads from the container execution logs.
- `kill_bash` maps to killing a process inside the container, rather than operating on host processes directly.
- For session-level containers, background tasks can persist across turns.
- For task-level containers, they are cleaned up by default when the turn ends.

## 15. Relationship with Audit and Rollback

The Docker backend should provide stronger audit capabilities.

Suggested records:

- image.
- image digest.
- container id.
- sandbox mode.
- mounts.
- network mode.
- env allowlist.
- resource limits.
- command.
- exit code.
- duration.
- stdout / stderr summary.
- changed files summary.
- git diff summary.

Optional enhancements:

- Create a git checkpoint before execution.
- Generate a diff after execution.
- Automatically save a patch for high-risk commands.
- Support `/sandbox diff`, `/sandbox reset`, `/sandbox logs`, `/sandbox status`.

## 16. Recommended Implementation Phases

### Phase 1: Abstract the Sandbox Runtime

- Keep the existing bwrap / sandbox-exec implementations.
- Introduce the `backend` concept.
- Extend the current `Sandbox` interface into a unified runtime.
- Keep `PathChecker` as an independent path layer.
- Add event and audit fields.

### Phase 2: Implement Docker Command Mode

- Support executing each command with `docker run --rm`.
- Mount the workspace at `/workspace`.
- Support network none / bridge.
- Support env allowlist.
- Support memory / cpu / pids limits.
- Support stdout / stderr streaming output.

### Phase 3: Implement Docker Session Mode

- Each Agent session creates one persistent container.
- Shell commands execute via `docker exec`.
- Support background tasks.
- Support cancellation.
- Support cleanup when the session ends.
- Support container recovery or cleanup after a daemon restart.

### Phase 4: Bring Hook / MCP / Subagent into the Docker Sandbox

- Hooks execute in the sandbox by default.
- stdio MCP can run inside containers.
- Subagent can bind to an independent sandbox profile.
- The extension manifest gains sandbox permission declarations.

### Phase 5: Default Policy Adjustments

- Daemon recommends Docker by default.
- YOLO requires a Docker or native sandbox by default.
- CI defaults to Docker + network none.
- High-risk commands get a strong warning when no sandbox is present.

## 17. Recommended Conclusions

- The current bwrap / sandbox-exec are suitable as lightweight native backends.
- Docker should be the more strongly recommended strong-isolation backend.
- High-risk scenarios such as YOLO, Daemon, CI, multi-Agent, MCP, and Hook should prefer Docker.
- The sandbox should be upgraded from a "Shell wrapper" to a "unified execution environment manager".
- The permission system and the sandbox system must be separate but work together.
- The Docker sandbox should support three lifecycles: command / task / session.
- All sandbox behavior must go into the event stream and audit log.
