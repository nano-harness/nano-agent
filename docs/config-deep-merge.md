# Configuration Deep Merge System

[中文](./config-deep-merge.zh-CN.md)

## Overview

Nano-agent now supports **deep merging** of configuration files from multiple layers, allowing settings from global user configuration to be combined with project-specific configuration instead of being replaced entirely.

## Configuration Layers

Configuration files are loaded and merged in the following priority order (lowest to highest):

1. **Defaults** - Built-in default configuration from `DefaultConfig()`
2. **User Config** - `~/.config/nano/config.yaml` (global settings)
3. **Project Config** - `.nano.yaml` (project-specific settings)
4. **Managed Settings** - Enterprise/managed settings (if present)
5. **Environment Variables** - Environment variable overrides

Higher layers override lower layers according to merge strategies (see below).

## Merge Strategies

Different fields use different merge strategies:

### Replace Strategy (Default)

For most scalar fields and atomic structures, higher layers completely replace lower layers:
- `api_key`, `base_url`, `model`
- `timeout`, `max_file_size`
- Entire nested objects without child policies

### Append Strategy

For list fields that should accumulate values from all layers:
- `security.allow_rules` - Security allow rules
- `security.deny_rules` - Security deny rules
- `allowed_commands`, `blocked_commands`
- `allowed_env_vars`, `blocked_env_vars`
- `enabled_tools`, `disabled_tools`
- `sensitive_read_paths`, `arbitrary_exec_commands`

Append strategy automatically deduplicates values.

### Merge-by-Key Strategy

For lists of objects where items should be merged by a unique key field:
- `security.hooks` (key: `name`) - Security hooks
- `mcp.servers` (key: `name`) - MCP server configurations
- `image_generator.providers` (key: `provider`) - Image generation providers

Items with the same key are merged recursively. Items with unique keys are appended.

## Examples

### Example 1: Security Hooks Merge

**User Config** (`~/.config/nano/config.yaml`):
```yaml
security:
  hooks:
    - name: "pre-commit"
      enabled: true
      command: "echo pre-commit hook"
    - name: "post-command"
      enabled: true
      command: "echo post-command hook"
  allow_rules:
    - "Bash(git *)"
```

**Project Config** (`.nano.yaml`):
```yaml
mcp:
  enable_client: true
security:
  hooks:
    - name: "pre-commit"
      enabled: false  # Override: disable the user's pre-commit hook
    - name: "project-specific"
      enabled: true
      command: "npm run validate"
  allow_rules:
    - "Bash(npm *)"
```

**Result**: All hooks are present with project overrides:
- `pre-commit`: **disabled** (overridden by project)
- `post-command`: **enabled** (from user, unchanged)
- `project-specific`: **enabled** (new from project)
- `allow_rules`: `["Bash(git *)", "Bash(npm *)"]` (appended)
- `mcp.enable_client`: `true` (from project)

### Example 2: MCP Server Merge

**User Config**:
```yaml
mcp:
  enable_client: true
  servers:
    - name: "filesystem"
      command: ["npx", "-y", "@modelcontextprotocol/server-filesystem", "/home"]
    - name: "github"
      command: ["npx", "-y", "@modelcontextprotocol/server-github"]
```

**Project Config**:
```yaml
mcp:
  servers:
    - name: "filesystem"
      command: ["npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
    - name: "project-db"
      command: ["node", "./db-mcp-server.js"]
```

**Result**: Three MCP servers:
- `filesystem`: Command updated to use `/tmp` (merged)
- `github`: Unchanged from user config
- `project-db`: New server from project config
- `enable_client`: `true` (inherited from user config)

### Example 3: Explicit Clear

Use empty arrays to explicitly clear a field:

**User Config**:
```yaml
security:
  hooks:
    - name: "hook1"
      enabled: true
```

**Project Config**:
```yaml
security:
  hooks: []  # Explicitly clear all hooks
```

**Result**: No hooks (explicitly cleared).

## Legacy Mode

For backward compatibility, you can temporarily revert to single-file selection behavior:

```bash
export NANO_CONFIG_LEGACY_SHADOW=1
nano
```

In legacy mode:
- If `.nano.yaml` exists, it is used exclusively (global config ignored)
- If `.nano.yaml` doesn't exist, `~/.config/nano/config.yaml` is used

**Note**: Legacy mode is deprecated and will be removed in a future release.

## Migration Guide

### Breaking Changes

**Before (Old Behavior)**:
- Project config completely replaced user config
- All global hooks, MCP servers, and rules were lost when project config existed

**After (New Behavior)**:
- Project config merges with user config
- Global hooks, MCP servers, and rules are preserved unless explicitly overridden or cleared

### Migration Steps

1. **Review your project configs**: If you have project `.nano.yaml` files that relied on implicit clearing of global settings, you may need to explicitly clear them using empty arrays:
   ```yaml
   security:
     hooks: []  # Explicitly clear if that's what you want
   ```

2. **Test with legacy mode first**: If uncertain, test with `NANO_CONFIG_LEGACY_SHADOW=1` to ensure the new behavior works for your use case.

3. **Update documentation**: Update any project README files that describe configuration behavior.

### Common Migration Scenarios

#### Scenario 1: Project intentionally had no hooks

**Old project config** (implicitly cleared global hooks):
```yaml
mcp:
  enable_client: true
```

**New behavior**: Global hooks are now inherited.

**Fix** (if you want to keep the old behavior):
```yaml
security:
  hooks: []  # Explicitly clear
mcp:
  enable_client: true
```

#### Scenario 2: Project wanted to completely replace security settings

**Old**: Security block in project config replaced all global security settings.

**New**: Security fields merge according to their strategies.

**Fix**: Explicitly clear each field you want to replace:
```yaml
security:
  hooks: []
  allow_rules: []
  deny_rules: []
```

## Implementation Details

### Merge Algorithm

The merge system uses a recursive algorithm with explicit merge policies:

1. Load all configuration layers into `map[string]interface{}`
2. Apply merge policies field-by-field based on path
3. For maps with child policies, recursively merge nested structures
4. For maps without child policies, replace entirely (default)
5. For lists, apply strategy: replace (default), append, or merge-by-key
6. Convert final merged map back to `Config` struct

### Policy Definitions

Merge policies are defined in `pkg/config/loader.go`:

```go
policies := map[string]merger.MergePolicy{
    "security.allow_rules": {Strategy: merger.StrategyAppend},
    "security.deny_rules":  {Strategy: merger.StrategyAppend},
    "security.hooks":       {Strategy: merger.StrategyMergeByKey, KeyField: "name"},
    "mcp.servers":          {Strategy: merger.StrategyMergeByKey, KeyField: "name"},
    // ... more policies
}
```

### Adding New Merge Policies

To add a new merge policy for a field:

1. Edit `buildMergePolicies()` in `pkg/config/loader.go`
2. Add the field path with the desired strategy
3. Add unit tests in `pkg/config/loader_test.go`

Example:
```go
policies["my_new_field.items"] = merger.MergePolicy{
    Strategy: merger.StrategyMergeByKey,
    KeyField: "id",
}
```

## Testing

### Unit Tests

```bash
# Test the merger package
go test ./pkg/config/merger/... -v

# Test config loading
go test ./pkg/config/... -v
```

### Integration Tests

See `pkg/config/loader_test.go` for comprehensive integration tests covering:
- User + project config merging
- Only user config
- Only project config
- Explicit empty array clearing
- Append strategy deduplication
- Merge-by-key with updates
- Legacy mode compatibility

## Troubleshooting

### Issue: My global hooks aren't being used

**Cause**: Project config has an empty `hooks: []` which explicitly clears them.

**Solution**: Remove the empty hooks array from project config, or add the hooks you want at the project level.

### Issue: Too many rules are being applied

**Cause**: Rules from both global and project configs are being appended.

**Solution**: Use `allow_rules: []` in project config to explicitly clear global rules, then add only the rules you want.

### Issue: Legacy behavior is needed temporarily

**Solution**: Set `NANO_CONFIG_LEGACY_SHADOW=1` environment variable.

## Future Work

- Remove `NANO_CONFIG_LEGACY_SHADOW` flag in next major release
- Add `nano config show` command to display final merged configuration
- Add `nano config debug` command to show which layer each value came from
- Support for `.nano.local.yaml` (git-ignored local overrides)
