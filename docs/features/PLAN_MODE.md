# Plan Mode

[中文](./PLAN_MODE.zh-CN.md)

Plan Mode is a read-only execution mode designed for safe code analysis and planning without the risk of modifying files or system state.

## Overview

Plan Mode restricts the agent to read-only operations, preventing any modifications to the filesystem, execution of state-changing shell commands, or other destructive actions. This is ideal for:

- **Code Analysis**: Explore and understand a codebase before making changes
- **Planning Phase**: Research and design implementation strategies
- **Safe Exploration**: Investigate unfamiliar codebases without risk
- **Review Scenarios**: Analyze code changes without modifying them

## Switching Permission Modes

nano-agent supports four permission modes:

- **default**: Requires user confirmation for tools that declare `RequiresConfirmation() == true`
- **acceptEdits**: Automatically approves filesystem write operations while still prompting for shell commands
- **plan**: Read-only mode - blocks all write operations and state-changing commands
- **yolo**: Skips all permission checks (use with caution)

### Activating Plan Mode

```bash
# Via slash command in interactive mode
/permission plan

# Or use the short form
/plan
```

### Exiting Plan Mode

```bash
# Switch to default mode
/permission default

# Or switch to acceptEdits for auto-approved file edits
/permission acceptEdits

# Or go full YOLO (not recommended for production)
/yolo
```

## What's Allowed in Plan Mode

Plan Mode permits the following read-only operations:

### File System Operations
- `read_file` - Read file contents
- `list_directory` - List directory contents
- `search_files` - Search for files by pattern
- `file_grep` - Search within file contents
- `glob_files` - Match files by glob pattern

### Code Analysis
- `codebase_search` - Search across the codebase
- `search_code` - Code-specific search
- `view_code` - View code sections

### Web Operations (Read-Only)
- `web_search` - Search the web
- `web_fetch` - Fetch web content

### Planning Tools
- `create_plan` - Create implementation plans
- `analyze_task` - Analyze task requirements

### Memory/Context Queries
- `search_memory` - Search conversation memory
- `list_memories` - List stored memories

### Read-Only Shell Commands
Plan Mode allows specific read-only shell commands (prefix match):
- `ls`, `cat`, `head`, `tail` - File viewing
- `grep`, `find` - Search operations
- `git status`, `git log`, `git diff`, `git show` - Git inspection (read-only)
- `pwd`, `which` - Path information
- `echo`, `env`, `printenv` - Environment inspection
- `stat`, `file`, `wc` - File information
- `sort`, `uniq` - Data processing
- `less`, `more`, `tree` - Viewing

Note: Plan mode determines whether a shell command is read-only by checking if
the command string starts with one of the allowed prefixes.

## What's Blocked in Plan Mode

The following operations require confirmation and will be blocked:

### File System Modifications
- ❌ `write_file` - Writing files
- ❌ `edit_file` - Editing files
- ❌ `delete_file` - Deleting files
- ❌ `create_directory` - Creating directories
- ❌ `move_file` - Moving files

### State-Changing Shell Commands
- ❌ `npm install` - Package installation
- ❌ `git commit` - Git commits
- ❌ `git push` - Git pushes
- ❌ `rm` - File deletion (except `rm` of single files, which is blocked regardless)
- ❌ `mv` - Moving files
- ❌ `cp` - Copying files
- ❌ `chmod` - Permission changes
- ❌ Build/compile commands
- ❌ Test execution

### System Modifications
- ❌ Package management commands
- ❌ System configuration changes
- ❌ Network modifications

## LLM Guidance

When Plan Mode is active, the LLM receives comprehensive guidance in the system prompt explaining:

1. **Current Mode**: That the agent is operating in Plan Mode
2. **Allowed Tools**: Explicit list of permitted read-only operations
3. **Prohibited Actions**: Clear list of blocked modifications
4. **Role in Plan Mode**: Guidance to analyze, plan, research, document, and report
5. **How to Exit**: Instructions for transitioning out of Plan Mode

This ensures the LLM understands the restrictions and adapts its behavior accordingly.

## Example Usage

```bash
# Start nano-agent
nano chat

# Switch to plan mode
/permission plan

# The agent can now safely explore the codebase
> Can you analyze the authentication flow in this codebase?

# Agent explores using read-only tools:
# - read_file src/auth/*.go
# - search_code "authentication"
# - git log --oneline src/auth/

# When ready to implement changes, exit plan mode
/permission default

# Now the agent can make modifications
> Please update the auth middleware to add rate limiting
```

## Integration with Hooks and Firewall

Plan Mode works alongside the dangerous command firewall to provide defense-in-depth:

1. **Plan Mode** blocks tools at the permission level
2. **Dangerous Command Firewall** catches risky commands even if they pass permission checks
3. **Hooks** can add additional validation logic

This layered approach ensures maximum safety during the planning phase.

## Configuration

Plan Mode is available out-of-the-box and requires no configuration. It's controlled entirely through slash commands during runtime.

For programmatic access:

```go
import "github.com/nano-harness/nano-agent/pkg/agent/permission"

// Create manager in Plan mode
mgr := permission.NewManager(permission.ModePlan, nil)

// Check if a tool requires confirmation
needsConfirm := mgr.ShouldConfirm(toolName, params, tool)

// Switch modes dynamically
mgr.SetMode(permission.ModeDefault)
```

## Best Practices

1. **Start with Plan Mode**: When exploring unfamiliar code, begin in Plan mode
2. **Design Before Implementation**: Use Plan mode to understand the codebase and design your approach
3. **Switch Deliberately**: Consciously switch to a more permissive mode only when ready to make changes
4. **Combine with Firewall**: Keep the firewall enabled even in other modes for additional safety

## Troubleshooting

### "Tool requires confirmation" in Plan Mode

If you see this message, it means the tool you're trying to use is not in the read-only whitelist. Common causes:

- Attempting to modify files (`write_file`, `edit_file`)
- Running build/test commands
- Executing state-changing shell commands

**Solution**: Either use a read-only alternative or exit Plan mode if modifications are needed.

### Read-only command blocked unexpectedly

If a command you believe is read-only gets blocked:

1. Check if it's in the whitelist (see "What's Allowed" above)
2. The command might have side effects (e.g., `git fetch` modifies local refs)
3. Custom hooks might be adding additional restrictions

**Solution**: Switch to `default` or `acceptEdits` mode, or add the command to the allowlist.

## Related Documentation

- [Permission Policy](../development/PERMISSION_POLICY.md)
- [Dangerous Command Firewall](./FIREWALL.md)
- [Hooks](./HOOKS.md)
- [Permission Auto-Approval](../development/PERMISSION_AUTO_APPROVAL.md)
