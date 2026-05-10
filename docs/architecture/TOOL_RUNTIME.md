# Tool Runtime

`pkg/toolruntime` is the stable runtime seam for tool metadata, catalog lookup, and execution.

## Components

- Metadata descriptors describe tool identity, category, schema, and execution capabilities.
- The catalog exposes normalized tool descriptors.
- The runtime executes tools through the registered tool implementation and middleware chain.

## Compatibility

Existing `pkg/tools` descriptor APIs remain compatibility aliases. Existing tool registration through toolbox code remains supported while execution delegates to `pkg/toolruntime.Runtime`.

## Execution expectations

Tool execution should:

1. resolve a registered tool by name;
2. validate parameters through the tool schema when available;
3. apply policy, sandbox, hook, and audit middleware;
4. return an `interfaces.ToolResult`;
5. emit public events through the surrounding agent/session runtime.

## Tool metadata expectations

Tool metadata should include:

- stable name;
- description;
- category;
- parameter schema;
- confirmation requirement;
- concurrency safety.

Tool metadata is used by CLI/TUI/daemon surfaces and extension manifests, so it should avoid UI-specific assumptions.

