# MCP Tool Lazy Loading (Tool Search Mode)

[中文](./MCP_TOOL_SEARCH.zh-CN.md)

Tool schemas are a hidden token tax: every registered tool definition is
shipped to the model on every request. The September 2026
harness-engineering field research measured ~67k tokens consumed by just
7+ configured MCP servers, and Anthropic's tool search report shows ~85%
token reduction for large tool libraries when schemas are deferred behind a
search meta-tool.

nano-agent implements **threshold-based lazy loading**: as long as the
aggregate size of MCP tool definitions is small, they are injected with full
schemas (zero extra round-trips); once they exceed a configurable share of
the context window, the agent switches to *tool search mode*.

## How it works

1. **Evaluation** (`pkg/agent/tool_search.go`) — whenever the tool inventory
   changes (built-in registration, asynchronous MCP (un)registration), the
   agent estimates the token cost of every MCP tool definition
   (name + description + JSON schema, ~4 chars/token) and compares the total
   against `threshold_ratio × context_window`.
2. **Eager mode (under threshold)** — MCP tools behave like core tools:
   full schemas in the system prompt and in the API tool list.
3. **Lazy mode (over threshold)** — full schemas are withheld. The model
   sees only:
   - the lightweight `discover_tools` meta-tool, and
   - a compact index in the system prompt (tool name + one-line description,
     grouped by server).
4. **Activation** — the agent calls `discover_tools` with a query or an exact
   name; the full schema is returned and the tool is marked *expanded* in the
   `ProgressiveDisclosure` gate, so its schema is exposed to the model from
   that point on (subject to the expansion eviction budget). Tool dispatch
   and the MCP client are untouched — execution works identically in both
   modes.

The decision is re-evaluated on every tool-inventory change, so adding or
removing MCP servers flips modes automatically.

## Configuration

```yaml
tool_search:
  enabled: true          # default: true; false forces eager mode
  threshold_ratio: 0.10  # fraction of the context window (default: 0.10,
                         # mirroring Claude Code's ~10% behavior)
  context_window: 0      # override the window size used for the threshold;
                         # default: context.model_context_window, else 200000
```

## Interaction with existing mechanisms

- The gating layer reuses the existing `ProgressiveDisclosure` index and the
  `discover_tools` meta-tool; `ToolSearchGate` only adds the threshold-driven
  eager/lazy switch on top.
- The tool scheduler's schema auto-injection path (retrying a call after
  fetching the schema) keeps working in lazy mode.
- Sub-agents are unaffected (they do not register the management tools).

## Implementation notes

- `EvaluateToolSearch` and `EstimateToolDefinitionTokens` are pure functions,
  covered by table-driven tests together with the gate's expose/hide matrix
  and the system-prompt rendering toggle.
- When lazy loading flips, the system prompt cache is invalidated so the next
  turn renders the right tool section.
