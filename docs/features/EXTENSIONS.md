# Extensions

The extension ecosystem normalizes skills, MCP servers, tools, agents, and commands into a common manifest.

## Manifest kinds

Supported extension kinds:

- `skill`
- `mcp`
- `tool`
- `agent`
- `command`

`manage_extension` can list manifests, show a manifest, diagnose extensions,
inspect trust/audit metadata, and manage supported install/update/remove
operations.

Supported actions:

- `list` / `status` - List all extensions or show status of a specific extension
- `manifest` - Display the manifest of an extension
- `install` / `update` - Install or update an extension
- `enable` / `disable` - Enable or disable an extension
- `remove` - Remove an extension
- `doctor` - Diagnose extension health issues
- `trust` - Manage trust metadata for extensions
- `audit` - View audit information for extensions

## Usage Examples

### List All Extensions

```json
{
  "action": "list"
}
```

### Show Extension Status

```json
{
  "action": "status",
  "name": "my-skill"
}
```

### View Extension Manifest

```json
{
  "action": "manifest",
  "name": "my-mcp-server"
}
```

### Install an Extension

```json
{
  "action": "install",
  "name": "example-skill",
  "source": "https://example.com/skills/example-skill.md"
}
```

### Enable/Disable an Extension

```json
{
  "action": "enable",
  "name": "my-skill"
}
```

```json
{
  "action": "disable",
  "name": "my-skill"
}
```

### Remove an Extension

```json
{
  "action": "remove",
  "name": "my-skill"
}
```

### Diagnose Extension Issues

```json
{
  "action": "doctor",
  "name": "problematic-extension"
}
```

## Trust model

Manifest trust metadata records whether an extension can be treated as trusted:

- `runtime`: registered in the current process.
- `local` / `configured`: loaded from local config or filesystem.
- `remote`: HTTPS remote source requiring explicit confirmation.
- `remote_insecure`: HTTP remote source requiring explicit confirmation and transport upgrade.

Remote skill and MCP install/update operations through `manage_extension` require an explicit confirmation handler. They must not be applied silently.
Runtime removal is limited to personal skill installations and configured MCP
servers; tools, agent profiles, and command extensions are removed from their
runtime registration or project declaration instead.

## Health and permissions

Manifests expose:

- health status and message;
- permissions requested or implied by the extension;
- source and metadata.

AgentProfile manifests include `permission_mode` and `allowed_tools`. Command manifests include `allowed-tools` and `permission-profile` metadata where present.

## Related docs

- [Extension Event Schema](../development/EXTENSION_EVENT_SCHEMA.md)
- [Configuration Guide](../development/CONFIGURATION.md)
- [Multi-Agent System](MULTI_AGENT.md)
