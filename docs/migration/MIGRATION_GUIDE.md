# Migration Guide

[中文](./MIGRATION_GUIDE.zh-CN.md)

This guide covers all migration scenarios for nano-agent, including architecture refactoring and feature upgrades.

## Table of Contents

1. [Architecture Refactor Migration](#architecture-refactor-migration)
2. [Single-Agent to Swarm Mode Migration](#single-agent-to-swarm-mode-migration)
3. [Version-Specific Migration](#version-specific-migration)

---

## Architecture Refactor Migration

This section maps the architecture refactor plan to compatibility-preserving migration steps.

### Migration Principles

- Keep old public entry points working while adding new seams.
- Migrate one call chain at a time.
- Prefer adapters and compatibility aliases before deleting old code.
- Add tests around public behavior before changing internals.
- Emit stable public events and audit records for new behavior.

### Completed Migration Seams

- Tool metadata and execution moved behind `pkg/toolruntime`.
- Hook execution moved behind `pkg/hookservice`.
- Turn execution gained an internal executor seam.
- Sessions gained explicit lifecycle state and incremental JSONL resume support.
- Extension manifests unified skill, MCP, tool, agent, and command metadata.
- Slash commands gained scoped tool/permission metadata.
- AgentProfiles added project-local configurable teammates.
- Daemon team sessions gained replay and approval compatibility frames.
- Configuration migration guidance lives in `docs/development/CONFIGURATION.md`.

### Configuration Migration

No migration is required for existing configs. New fields and directories are additive:

- `.nano/commands` and compatible `.claude/commands` for slash commands.
- `.nano/agents` for AgentProfiles.
- command frontmatter `allowed-tools` and `permission-profile`.
- AgentProfile fields `permission_mode` and `allowed_tools`.

### Validation Checklist

Before merging refactor changes:

- Run lint and unit tests.
- Run focused tests for the packages touched by the change.
- Run full e2e tests for daemon/session/tool behavior when those surfaces change.
- Run security validation for permission, hook, extension, sandbox, and audit changes.

---

## Single-Agent to Swarm Mode Migration

This guide helps you migrate from single-agent nano-agent usage to the new Swarm multi-agent system.

### v3 → v4: EventSource TUI and Daemon Client Migration

#### Removed Commands and Replacements

- `nano lead-chat` no longer uses readline/plain text rendering; it starts the BubbleTea TUI by default.
- Use `nano lead-chat --ui tview` to select the tview backend.
- Scripts and CI must use `nano daemon execute --json "your command"` instead of parsing interactive stream output.

#### Adapter Interface Migration

Old UI adapters implemented `Run`, `SendEvent`, `SubmitChannel`, and `CancelChannel`. New adapters implement only `Run(ctx, EventSource) error` and `Stop`. Put execution, cancel, approval, reset, and session listing behavior into an `eventsource.EventSource` implementation.

#### Daemon Rendering Migration

Do not render daemon WebSocket frames with `fmt.Print` in CLI paths. Build an EventSource over the daemon WebSocket, feed inbound frames to BubbleTea/tview, and send user actions back as submit/cancel/approval/control outbound messages.

### Overview

Swarm mode introduces team-based agent coordination, allowing a team-lead agent to spawn and coordinate multiple teammate agents. This guide covers:

1. Understanding the differences
2. Updating your workflows
3. Migrating existing code
4. New capabilities

### What's Changed

#### Phase 1: Runtime Layer (Completed)

**Mailbox System**:
- New mailbox infrastructure for inter-agent communication
- Filesystem-based message storage under `~/.nano/teams/<team>/mailbox/` (legacy `~/.nano-agent/` paths are auto-migrated on first use)
- Tools: `send_message` for teammates

**Impact**: Minimal - Mailbox is optional and doesn't affect single-agent usage

#### Phase 2: Agent Tools Layer (Completed)

**New Tools**:
- `main_agent`: Execute tasks using main agent capabilities
- `spawn_teammate`: Team-leads can spawn teammate agents (future)
- `send_message`: Teammates can send messages to team-lead

**Changes**:
- `task` tool removed (replaced by `main_agent`)
- `fork` tool removed (superseded by spawn_teammate)

**Impact**: Medium - If you used `task` or `fork` tools, you need to update

#### Phase 3: Daemon & TUI Integration (Current)

**Daemon Enhancements**:
- Team-lead session management via HTTP API
- Long-running team sessions with mailbox support
- WebSocket team REPL stream via `/api/v1/teams/sessions/{id}/stream`
- `nano lead-chat` daemon-backed REPL client using `lead_input` frames
- REPL-driven daemon tool confirmation using `waiting_for_user` and `tool_approval` frames
- Automatic session cleanup after idle timeout

**TUI Enhancements**:
- `--team` flag for TUI modes
- `nano chat --team <name>` for team-lead REPL
- `nano lead-chat --team <name>` for daemon-backed resumable team-lead REPL
- `nano teammate` command for subprocess execution

**Impact**: Low - New features are additive, existing functionality unchanged

#### Phase 4: Runtime Home Unification (Breaking Change)

**Runtime Home Directory**:
- All runtime state previously stored under `~/.nano-agent/` is now centralized under `~/.nano/`
- Team state lives at `~/.nano/teams/<team-name>/`
- Team mailboxes live at `~/.nano/teams/<team-name>/mailbox/`
- Daemon sessions live at `~/.nano/sessions/`

**Automatic Migration**:
- On first use, `~/.nano-agent/` contents (excluding `README.md`) are moved into `~/.nano/`
- A `README.md` stub is left in `~/.nano-agent/` pointing to the new location
- Migration is idempotent and runs once per user home (guarded by `sync.Once`)
- See `pkg/runtime/paths.go::MigrateLegacyPaths` for the implementation

**Action Required**:
- Update any tooling that hard-coded paths under `~/.nano-agent/` to use `~/.nano/`
- Update mailbox `root_dir` config entries to point at `~/.nano/teams/<team>/mailbox` (or leave empty to use the default)
- Remove `~/.nano-agent/` once you have confirmed `~/.nano/` contains the migrated state

**Impact**: Medium - File paths change, but state is moved automatically on first use

### Migration Paths

#### Path 1: Continue Using Single-Agent Mode (No Changes Required)

If you don't need multi-agent coordination, **no migration is required**. Single-agent mode works exactly as before:

```bash
# These commands work unchanged
nano "fix the bug"
nano --tui
nano daemon start
```

The new swarm features are opt-in and don't affect existing workflows.

#### Path 2: Adopt Team-Lead Mode (Gradual Migration)

Migrate gradually by using team-lead mode for complex tasks:

**Before (Single Agent)**:
```bash
nano "analyze the entire codebase for security issues"
```

Single agent processes everything sequentially.

**After (Team-Lead)**:
```bash
nano --team security-team --tui
```

Then in the TUI:
```
analyze the codebase for security issues - spawn teammates to check different modules
```

The team-lead can delegate to teammates for parallel processing.

#### Path 3: Full Swarm Adoption (Advanced)

Use the daemon API for programmatic team management:

```python
import requests

# Create team session
response = requests.post('http://localhost:4380/api/v1/teams/sessions', json={
    'team_name': 'security-team'
})
session_id = response.json()['session_id']

# Execute with team coordination
requests.post(f'http://localhost:4380/api/v1/teams/sessions/{session_id}/execute', json={
    'command': 'comprehensive security audit with parallel module analysis'
})
```

### Specific Migration Scenarios

#### Scenario 1: You Used the `task` Tool

**Before (Phase 1)**:
```json
{
  "tool": "task",
  "parameters": {
    "instruction": "analyze code quality",
    "context": "focus on main.go"
  }
}
```

**After (Phase 2+)**:
```json
{
  "tool": "main_agent",
  "parameters": {
    "task": "analyze code quality in main.go"
  }
}
```

**Why Changed**: `main_agent` provides clearer semantics and better aligns with the team-lead/teammate architecture.

#### Scenario 2: You Used the `fork` Tool

**Before (Phase 1)**:
```json
{
  "tool": "fork",
  "parameters": {
    "session_id": "parallel-task-1",
    "command": "run tests"
  }
}
```

**After (Phase 3+)**:
Use `spawn_teammate` in team-lead mode:
```json
{
  "tool": "spawn_teammate",
  "parameters": {
    "name": "test-runner",
    "task": "run all unit tests and report results"
  }
}
```

**Why Changed**: Teammates provide better lifecycle management, automatic mailbox integration, and clearer semantics.

### Breaking Changes

#### Phase 2 Breaking Changes

1. **Removed Tools**:
   - `task` → Use `main_agent` instead
   - `fork` → Use `spawn_teammate` in team-lead mode

2. **Tool Registration**:
   - `RegisterAgentTools()` now only registers `main_agent` and `send_message`
   - Swarm tools (spawn_teammate, etc.) registered separately in team-lead mode

#### API Compatibility

The daemon API remains backward compatible. New endpoints are additive:

**Existing APIs (unchanged)**:
```
POST /api/v1/sessions/execute
GET  /api/v1/sessions
GET  /api/v1/sessions/{id}
DELETE /api/v1/sessions/{id}
```

**New APIs (opt-in)**:
```
POST /api/v1/teams/sessions
GET  /api/v1/teams/sessions
GET  /api/v1/teams/sessions/{id}
POST /api/v1/teams/sessions/{id}/execute
DELETE /api/v1/teams/sessions/{id}
```

### Configuration Updates

#### No Config Changes Required

Swarm features work with existing configuration. Optional enhancements:

```yaml
# Optional: Disable team sessions in daemon
# Set environment variable NANO_DISABLE_TEAM_SESSIONS=true

# Optional: Configure mailbox (defaults shown)
mailbox:
  enabled: true
  backend: "file"  # or "memory" for CLI sessions
  root_dir: "~/.nano/teams/<team>/mailbox"  # optional override
  max_per_agent: 1000
```

### Testing Your Migration

#### 1. Test Single-Agent Mode Still Works

```bash
# Should work unchanged
nano "simple task"
nano --tui "interactive task"
```

#### 2. Test Team-Lead Mode

```bash
# Test chat command
nano chat --team test-team
> exit

# Test TUI with team flag
nano --team test-team --help
```

#### 3. Test Daemon Team Sessions

```bash
# Start daemon
nano daemon start

# Test team session API
curl -X POST http://localhost:4380/api/v1/teams/sessions \
  -H "Content-Type: application/json" \
  -d '{"team_name": "test"}'
```

#### 4. Run Tests

```bash
# Run E2E tests
go test -tags=e2e ./e2e -run "TeamSession|Swarm" -v
```

### Rollback Plan

If you encounter issues with swarm features:

#### Option 1: Disable Team Sessions in Daemon

```bash
NANO_DISABLE_TEAM_SESSIONS=true nano daemon start
```

#### Option 2: Use Previous Version

```bash
# Checkout specific version before swarm
git checkout <commit-before-swarm>
go build ./cmd/nano
```

#### Option 3: Avoid Team Features

Simply don't use:
- `--team` flag
- `nano chat` command
- Team session API endpoints

Single-agent mode is unaffected.

### Common Issues and Solutions

#### Issue 1: "main_agent tool not found"

**Cause**: Using Phase 1 code with Phase 2+ expectations

**Solution**: Ensure your agent properly registers tools:
```go
import "github.com/nano-harness/nano-agent/pkg/tools/agent"

// Register agent tools
agent.RegisterAgentTools(registry, cfg, mainAgent)
```

#### Issue 2: Mailbox permission errors

**Cause**: Incorrect permissions on `~/.nano/teams/<team>/mailbox/` directory

**Solution**:
```bash
chmod -R 755 ~/.nano/teams/
```

#### Issue 3: Team sessions not appearing in list

**Cause**: Sessions may have been cleaned up due to idle timeout

**Solution**: Check timeout configuration or execute commands more frequently

#### Issue 4: Teammate commands failing

**Cause**: Missing required flags

**Solution**: Ensure all required flags are provided:
```bash
nano teammate --team alpha --name worker-1 \
  --session sess_123 --initial-prompt-file /tmp/prompt.txt
```

---

## Version-Specific Migration

### Getting Help

- **Documentation**: See [Multi-Agent Runtime](../features/MULTI_AGENT.md) for detailed swarm documentation
- **Issues**: Report issues at https://github.com/nano-harness/nano-agent/issues
- **Examples**: Check `e2e/team_session_test.go` for working examples

### Summary

- **No forced migration**: Single-agent mode works unchanged
- **Opt-in features**: Team functionality is additive
- **Gradual adoption**: Use `--team` flag when you need coordination
- **Backward compatible**: Existing APIs and commands work as before
- **New capabilities**: Parallel execution, specialized teammates, status updates

The swarm system enhances nano-agent without disrupting existing workflows. Adopt it gradually as your needs grow.
