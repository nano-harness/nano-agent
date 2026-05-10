# Dangerous Command Firewall

The Dangerous Command Firewall is a built-in security feature that detects and intercepts potentially dangerous shell commands before they execute, providing an additional layer of protection beyond permission modes.

## Overview

The firewall operates as a pre-execution hook that analyzes shell commands against a comprehensive set of dangerous patterns. When a risky command is detected, the firewall can:

- **Confirm**: Ask the user for explicit approval (default)
- **Block**: Refuse execution entirely
- **Allow**: Log the warning but permit execution (for testing/development)

The firewall provides **defense-in-depth** security by catching dangerous commands even if they pass other permission checks.

## Automatic Enablement

The firewall is registered automatically as a built-in programmatic `pre_tool_use` hook during agent startup. No shell hook configuration is required. It is enabled by default and can be disabled with:

```yaml
firewall:
  enabled: false
```

## Built-in Dangerous Patterns

The firewall includes patterns for three severity levels:

### High Severity (Destructive Commands)

These commands can cause irreversible damage to the system:

- **`rm -rf /`** - Recursive deletion of root directory
- **`rm -rf ~`** - Recursive deletion of home directory
- **`:(){ :|:& };:`** - Fork bomb attempts
- **`dd if=/dev/zero of=/dev/sda`** - Direct disk write operations
- **`mkfs.*`** - Filesystem formatting
- **`chmod -R 777`** - Setting world-writable permissions
- **`chown -R ... /`** - Recursive ownership change on root

### Medium Severity (VCS & System Operations)

These commands can lead to data loss or difficult recovery:

- **`git push --force`** - Force push to remote repository
- **`git push ... main|master`** - Push to protected branches
- **`git reset --hard`** - Hard reset to previous commit
- **`git clean -fd`** - Force clean untracked files
- **`sudo rm|mv|cp|dd|mkfs|chmod|chown`** - Privileged file operations
- **`> /dev/sd*`** - Output redirect to block device

### Low Severity (Package Management)

These commands can affect system packages:

- **`apt|yum|dnf remove`** - Package removal
- **`npm uninstall -g`** - Global package removal

## Configuration

### YAML Configuration

Configure the firewall in your `config.yaml`:

```yaml
# Dangerous Command Firewall Configuration
firewall:
  enabled: true                    # Enable/disable the firewall
  severity_threshold: medium       # Only flag commands at or above this severity
  failure_policy: confirm          # What to do when dangerous command detected: confirm, block, or allow

  # Optional: Add custom dangerous patterns
  custom_patterns:
    - pattern: "dropdb.*"          # Regex pattern to match
      severity: high               # Severity level: high, medium, or low
      category: database           # Category for grouping (e.g., database, security, vcs)
      reason: "Database deletion"  # Human-readable explanation

    - pattern: "kubectl delete.*production"
      severity: high
      category: kubernetes
      reason: "Production k8s deletion"

  # Optional: Override/whitelist specific commands
  overrides:
    - "rm -rf /tmp/test"           # Exact command strings to always allow
    - "git push --force origin my-feature-branch"
```

### Configuration Options

#### `enabled` (bool)
- **Default**: `true`
- **Description**: Master switch for the firewall. When `false`, all commands pass without firewall checks.

#### `severity_threshold` (string)
- **Default**: `medium`
- **Options**: `low`, `medium`, `high`
- **Description**: Only commands at or above this severity trigger the firewall action.
  - `low`: Catch everything including package management
  - `medium`: Catch destructive and VCS operations (recommended)
  - `high`: Only catch highly destructive operations

#### `failure_policy` (string)
- **Default**: `confirm`
- **Options**: `confirm`, `block`, `allow`
- **Description**: Action to take when a dangerous command is detected:
  - `confirm`: Ask user for approval (recommended for development)
  - `block`: Refuse execution entirely (recommended for CI/production)
  - `allow`: Log warning but permit execution (testing only)

#### `custom_patterns` (array)
- **Default**: `[]` (empty, uses only built-in patterns)
- **Description**: Add organization-specific dangerous patterns.
- **Fields**:
  - `pattern`: Go regex pattern (will be compiled with `regexp.MustCompile`)
  - `severity`: `high`, `medium`, or `low`
  - `category`: Arbitrary category string for grouping
  - `reason`: Human-readable explanation shown to user

#### `overrides` (array)
- **Default**: `[]` (empty)
- **Description**: Exact command strings that bypass firewall checks.
- **Use case**: When you have a specific dangerous command that's safe in your context.
- **Warning**: Use sparingly - overrides defeat the purpose of the firewall.

## Default Configuration

If no firewall configuration is provided, nano-agent uses these defaults:

```yaml
firewall:
  enabled: true
  severity_threshold: medium
  failure_policy: confirm
  custom_patterns: []
  overrides: []
```

## Usage Examples

### Example 1: Default Configuration (Recommended for Development)

```yaml
firewall:
  enabled: true
  severity_threshold: medium
  failure_policy: confirm
```

**Behavior**:
- Safe commands execute immediately: `ls`, `git status`, `cat file.txt`
- Dangerous commands prompt for approval: `rm -rf /tmp/data`, `git push --force`
- Low-severity commands pass: `apt install` (below medium threshold)

### Example 2: Strict Production Configuration

```yaml
firewall:
  enabled: true
  severity_threshold: low
  failure_policy: block
```

**Behavior**:
- All dangerous commands are blocked, including package management
- No user prompts - immediate denial
- Suitable for automated environments where dangerous commands should never run

### Example 3: Development with Custom Patterns

```yaml
firewall:
  enabled: true
  severity_threshold: medium
  failure_policy: confirm

  custom_patterns:
    # Protect production databases
    - pattern: "psql.*DROP DATABASE"
      severity: high
      category: database
      reason: "Dropping database"

    # Catch dangerous Terraform commands
    - pattern: "terraform destroy"
      severity: high
      category: infrastructure
      reason: "Infrastructure destruction"

    # Warn about potential secrets
    - pattern: "export.*PASSWORD="
      severity: medium
      category: security
      reason: "Exporting password in plaintext"

  # Allow specific safe variants
  overrides:
    - "terraform destroy -target=module.test"
```

### Example 4: Firewall Disabled (Not Recommended)

```yaml
firewall:
  enabled: false
```

**Behavior**:
- No command checking occurs
- All commands execute based on permission mode only
- **Warning**: Only disable if you have other security controls in place

## How It Works

### Execution Flow

1. **Tool Invocation**: Agent requests to execute a shell command
2. **Hook Trigger**: Built-in programmatic firewall hook fires on `pre_tool_use` event
3. **Command Extraction**: Extract command string from tool parameters
4. **Override Check**: Check if command is in override whitelist
5. **Pattern Matching**: Match command against built-in + custom patterns
6. **Severity Check**: Verify if matched pattern meets severity threshold
7. **Policy Application**: Apply configured failure policy
8. **User Notification**: Display warnings and reason to user
9. **Decision**: Allow, confirm, or block based on policy

### Integration with Permission Modes

The firewall works with all permission modes:

| Permission Mode | Firewall Enabled | Result |
|----------------|------------------|---------|
| `default` | Yes | Safe commands allowed, dangerous commands catch by firewall |
| `acceptEdits` | Yes | File edits allowed, dangerous shell commands caught by firewall |
| `plan` | Yes | Plan mode blocks most commands, firewall adds extra protection |
| `yolo` | Yes | YOLO bypasses permission checks, but firewall still runs |

**Defense-in-Depth**: The firewall provides a secondary safety net even when permission modes are permissive.

### Programmatic Hook Integration

The firewall uses the middleware `ProgrammaticHook` interface, so it runs in the same hook engine as user-defined shell/HTTP hooks without invoking an external process. External hooks run first; if they allow execution, the built-in firewall then evaluates shell commands and returns `allow`, `confirm`, or `block` according to `firewall.failure_policy`.

## Warning Messages

When a dangerous command is detected, users see detailed warnings:

```
⚠️  Dangerous command detected
Command: rm -rf /tmp/important
Reason: recursive force deletion (rm -rf)
Severity: high
Category: destructive

[Confirm] [Reject]
```

## Programmatic Access

### Using the Firewall in Code

```go
import (
    "github.com/nano-harness/nano-agent/pkg/agent/permission"
    "github.com/nano-harness/nano-agent/pkg/hookservice"
)

// Create firewall configuration
config := permission.FirewallConfig{
    Enabled:           true,
    SeverityThreshold: permission.SeverityMedium,
    FailurePolicy:     "confirm",
    CustomPatterns:    []permission.DangerousCommandRule{
        {
            Pattern:  regexp.MustCompile(`dropdb`),
            Severity: permission.SeverityHigh,
            Category: "database",
            Reason:   "Database deletion",
        },
    },
}

// Create firewall hook
firewallHook := permission.NewFirewallHook(config)

// Execute hook on command
params := map[string]interface{}{
    "command": "rm -rf /tmp/test",
}
decision, err := firewallHook.Execute(
    ctx,
    hookservice.EventPreToolUse,
    "run_shell_command",
    params,
)

// Check decision
switch decision.Action {
case hookservice.ActionAllow:
    // Execute command
case hookservice.ActionConfirm:
    // Ask user for approval
case hookservice.ActionBlock:
    // Refuse execution
}
```

### Checking Individual Commands

```go
import "github.com/nano-harness/nano-agent/pkg/agent/permission"

// Check if a command is dangerous
command := "git push --force origin main"
rule, isDangerous := permission.CheckCommand(command)

if isDangerous {
    fmt.Printf("Dangerous: %s\n", rule.Reason)
    fmt.Printf("Severity: %s\n", rule.Severity)
    fmt.Printf("Category: %s\n", rule.Category)
}
```

### Adding Custom Rules at Runtime

```go
// Create firewall with initial config
config := permission.DefaultFirewallConfig()

// Add custom patterns
config.CustomPatterns = append(config.CustomPatterns,
    permission.DangerousCommandRule{
        Pattern:  regexp.MustCompile(`heroku.*destroy`),
        Severity: permission.SeverityHigh,
        Category: "cloud",
        Reason:   "Heroku app destruction",
    },
)

hook := permission.NewFirewallHook(config)
```

## Sensitive File Detection

In addition to command detection, the firewall provides sensitive file pattern matching:

```go
// Check if a file path is sensitive
isSensitive := permission.IsSensitiveFile(".env")
// Returns: true

isSensitive = permission.IsSensitiveFile("README.md")
// Returns: false
```

### Built-in Sensitive Patterns

- `*.env`, `.env*` - Environment files
- `*.pem`, `*.key`, `*.p12`, `*.pfx` - Private keys
- `*credentials*`, `*secrets*`, `*password*` - Credential files
- `.kube/config`, `.aws/credentials` - Cloud config
- `.ssh/*`, `.gnupg/*` - SSH and GPG keys
- `id_rsa*`, `id_ed25519*` - SSH keys
- `*.crt`, `*.cer` - Certificates

## Testing

### Unit Tests

Test individual dangerous patterns:

```bash
go test ./pkg/agent/permission -run TestCheckCommand
```

### Integration Tests

Test the complete firewall flow:

```bash
go test -tags=integration ./pkg/agent/permission -run TestM1
```

## Best Practices

1. **Keep Firewall Enabled**: Always keep the firewall enabled, even with permissive permission modes
2. **Use Confirm Policy**: For development, `confirm` provides the best balance of safety and usability
3. **Threshold Medium**: Start with `medium` threshold and adjust based on your needs
4. **Minimize Overrides**: Only add overrides for commands you've carefully vetted
5. **Custom Patterns**: Add patterns for organization-specific dangerous operations
6. **Review Warnings**: Don't blindly approve dangerous commands - read the warnings
7. **Audit Logs**: Enable audit logging to track which dangerous commands were approved

## Troubleshooting

### Firewall Blocking Safe Commands

**Symptom**: A safe command is being flagged as dangerous.

**Solutions**:
1. Check if it matches a pattern unintentionally (e.g., `rm README.txt` matches `rm` pattern)
2. Add to `overrides` if the specific command is truly safe in your context
3. Lower the `severity_threshold` if the pattern is too aggressive
4. Contact maintainers if a built-in pattern is incorrect

### Firewall Not Catching Dangerous Commands

**Symptom**: A dangerous command executes without warning.

**Solutions**:
1. Verify `firewall.enabled: true` in config
2. Check that `severity_threshold` isn't set too high
3. Review the command pattern - it might not match built-in rules
4. Add a custom pattern for the specific command type
5. Ensure the built-in programmatic firewall hook has not been disabled in configuration

### Custom Patterns Not Working

**Symptom**: Custom patterns aren't matching commands.

**Solutions**:
1. Verify regex syntax - use word boundaries `\b` where appropriate
2. Test regex with Go's `regexp` package syntax
3. Check that `pattern` is a valid regex (e.g., `\bdropdb\b` not `dropdb*`)
4. Ensure config file is loaded correctly (`nano config show`)

## Related Documentation

- [Permission Policy](../development/PERMISSION_POLICY.md)
- [Plan Mode](./PLAN_MODE.md)
- [Hooks](./HOOKS.md)
- [Permission Auto-Approval](../development/PERMISSION_AUTO_APPROVAL.md)
