# Documentation Index

[中文](./INDEX.zh-CN.md)

Welcome to the nano-agent documentation. This index helps you navigate through all available documentation organized by category.

## Getting Started

- [Main README](../README.md) - Project overview, features, and quick start guide
- [README (Chinese)](../README.zh-CN.md) - Project overview, features, and quick start guide (Chinese)
- [CHANGELOG](../CHANGELOG.md) - Version history and release notes

## Architecture

Core architecture and design documentation:

- [Architecture Overview](architecture/ARCHITECTURE.md) - Refactored architecture summary and subsystem overview
- [Sandbox Design](architecture/SANDBOX_DESIGN.md) - Security sandbox implementation details
- [Tool Runtime](architecture/TOOL_RUNTIME.md) - Tool metadata, catalog, and execution runtime

## Features

Feature-specific documentation:

- [Multi-Agent System](features/MULTI_AGENT.md) - Multi-agent runtime and team coordination
- [Swarm System](features/SWARM.md) - Swarm multi-agent coordination details
- [Mailbox System](features/MAILBOX.md) - Inter-agent communication infrastructure
- [Intelligent Orchestration](features/INTELLIGENT_ORCHESTRATION.md) - Smart task distribution and sub-agent routing
- [Hooks System](features/HOOKS.md) - Hook loading and execution service
- [Extensions](features/EXTENSIONS.md) - Extension manifests for skills, MCP servers, tools, and agents
- [MCP OAuth](features/MCP_OAUTH.md) - OAuth integration for MCP servers

## Operations

Deployment, daemon, and operations documentation:

- [Daemon API](operations/DAEMON_API.md) - HTTP/WebSocket session APIs, replay, cancellation, and approval
- [Daemon Runtime](operations/DAEMON_RUNTIME.md) - Daemon lifecycle and runtime behavior
- [WebSocket Chunking](operations/WEBSOCKET_CHUNKING.md) - WebSocket message chunking protocol
- [Deployment Guide](../deployment/README.md) - AWS EC2 deployment instructions

## Development

Developer guides and configuration:

- [Configuration Guide](development/CONFIGURATION.md) - Configuration options and migration guidance
- [LLM Error Handling](../NANO.md) - Retry/failback/circuit-breaker classification and truncation metadata
- [Permission Policy](development/PERMISSION_POLICY.md) - Permission modes, approval, audit, and security
- [Permission Auto-Approval](development/PERMISSION_AUTO_APPROVAL.md) - Automatic approval for safe operations
- [Event Schema](development/EVENT_SCHEMA.md) - Public event envelopes and stream events
- [Extension Event Schema](development/EXTENSION_EVENT_SCHEMA.md) - Extension-specific event schemas
- [Agent Implementation](development/AGENT_IMPLEMENTATION.md) - Agent orchestration and implementation details

## Migration

Migration guides for upgrading between versions:

- [Migration Guide](migration/MIGRATION_GUIDE.md) - Comprehensive migration guide including:
  - Architecture refactor migration
  - Single-agent to swarm mode migration
  - Version-specific migration instructions

## Integration

Cross-repository contracts and interop documentation:

- [Symphony Interop Contract](symphony-interop.md) - Interface contract with nano-symphony orchestrator (exit codes, stdout JSON, file artifacts, flag priority)
- [Sandbox × Permission Matrix](sandbox-permission-matrix.md) - Flag combination behavior matrix across platforms

## Testing

Testing infrastructure and guidelines:

- [E2E Testing](../e2e/README.md) - End-to-end testing infrastructure
- [SWE-bench Testing](../swe_bench_test/README.md) - SWE-bench evaluation scripts
- [SWE-bench Testing (Chinese)](../swe_bench_test/README.zh-CN.md) - SWE-bench evaluation scripts (Chinese)

## References

Additional reference materials:

- [Claude Code & Gemini CLI Advanced Guide](references/Claude%20Code%20与%20Gemini%20CLI%20高级用法与黑科技实战指南.pdf) - Advanced usage guide (PDF, Chinese)

## Quick Links by Use Case

### I want to...

**Get started with nano-agent**
- Start with [Main README](../README.md)
- Review [Configuration Guide](development/CONFIGURATION.md)

**Deploy nano-agent**
- See [Deployment Guide](../deployment/README.md)
- Review [Daemon API](operations/DAEMON_API.md)

**Understand the architecture**
- Read [Architecture Overview](architecture/ARCHITECTURE.md)
- Review [Tool Runtime](architecture/TOOL_RUNTIME.md)

**Use multi-agent features**
- Read [Multi-Agent System](features/MULTI_AGENT.md)
- Check [Swarm System](features/SWARM.md)
- Review [Mailbox System](features/MAILBOX.md)

**Extend nano-agent**
- Review [Extensions](features/EXTENSIONS.md)
- Check [Hooks System](features/HOOKS.md)
- Read [Agent Implementation](development/AGENT_IMPLEMENTATION.md)

**Migrate from older versions**
- Follow [Migration Guide](migration/MIGRATION_GUIDE.md)

**Contribute to development**
- Review [E2E Testing](../e2e/README.md)
- Check [Configuration Guide](development/CONFIGURATION.md)
- Read [Event Schema](development/EVENT_SCHEMA.md)

## Documentation Organization

The documentation is organized into the following categories:

- **architecture/** - Core system architecture and design
- **features/** - Feature-specific documentation
- **operations/** - Deployment and operations
- **development/** - Developer guides and references
- **migration/** - Migration and upgrade guides
- **references/** - Additional reference materials

## Contributing

When adding new documentation:

1. Place it in the appropriate category directory
2. Update this index with a link
3. Use clear, descriptive titles
4. Include cross-references to related docs
5. Follow the existing documentation style

## Getting Help

- **Issues**: https://github.com/nano-harness/nano-agent/issues
- **Documentation**: This index and linked documents
- **Examples**: See `e2e/` directory for working examples
