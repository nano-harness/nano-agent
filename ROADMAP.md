# Roadmap

[中文](./ROADMAP.zh-CN.md)

This roadmap lists the directions nano-agent is actively exploring. Items are unordered and carry **no date commitments** — they ship when they are ready.

## Benchmarking

- **Full SWE-bench Verified run**: extend the current 31-instance subset (see [docs/testing/SWE_BENCH.md](./docs/testing/SWE_BENCH.md)) to the full 500-instance set, with per-instance duration, token usage, and cost detail published alongside results.
- **pass^k statistics**: repeated runs per instance to report variance and stability, not just a single pass@1 number.

## Context engineering

- **Context editing**: smarter in-conversation context management beyond truncation — selective pruning and rewriting of stale tool output.
- **Artifact tracking during compression**: keep references to files, patches, and other artifacts stable across context compression so earlier work stays addressable after compaction.

## Observability

- **OpenTelemetry export**: emit traces and metrics following the `gen_ai.*` semantic conventions (spans per LLM call and tool execution, token usage attributes) so nano-agent plugs into existing observability stacks.

## Tooling

- **MCP tool lazy loading**: defer MCP server connections and tool schema loading until a tool is actually needed, reducing startup cost and initial context footprint for configurations with many MCP servers.

## How to influence the roadmap

Open an issue or discussion on [GitHub](https://github.com/nano-harness/nano-agent/issues), or pick an item and send a PR — see [CONTRIBUTING.md](./CONTRIBUTING.md).
