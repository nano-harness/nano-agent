# Context Engineering: Context Editing & Artifact Tracking

[中文](./CONTEXT_ENGINEERING.zh-CN.md)

Two complementary context-engineering mechanisms in `pkg/agent`, informed by
the September 2026 harness-engineering field research:

1. **Context editing** (`context_editing.go`) — a lightweight, deterministic
   companion to compaction that clears stale tool results as the context
   approaches the budget.
2. **Artifact tracking** (`artifact_tracking.go`) — a deterministic manifest
   of files written/modified/deleted during the session, attached to every
   compression so the agent can keep doing correct incremental edits
   afterwards.

## Context editing

### Why

Compaction replaces a whole history segment with an LLM-generated summary. It
is powerful but expensive (an extra LLM call), lossy, and it rewrites the
context prefix — invalidating downstream prompt caches. Production evidence
shows a cheaper first line of defense: most of the token mass in a long
session is *stale tool results* (file reads, search hits, command output)
that the model no longer needs verbatim. Clearing them is deterministic,
free, and often sufficient on its own.

### Design constraints

- **Stable boundaries only.** Editing happens per whole *turn* (one user
  message plus everything up to the next user message). Message count, order,
  and roles never change — only the `content` of stale `tool` messages is
  replaced in place. A partial edit inside a turn would break the prompt-cache
  prefix unpredictably, so a turn is either fully edited or left intact.
- **Idempotent, deterministic placeholders.** Cleared results become
  `[tool result cleared: <tool_name>, <n> bytes]`. Already-cleared results are
  skipped on later passes, so an edited prefix stays byte-identical and warm
  in the prompt cache. Results smaller than the placeholder are never grown.
- **Editing before compaction.** The editor triggers at a lower utilization
  ratio (`edit_trigger_ratio`, default 0.5) than compaction (typically ≥0.7).
  It runs in `Turn.maybeEditContext` right before the compaction check in
  `requestOpenAIAPI`; compaction remains the fallback when editing alone
  cannot bring the context back under budget.

### Configuration

```yaml
context:
  enable_context_editing: true   # master switch (default: true)
  edit_trigger_ratio: 0.5        # edit when usage exceeds 50% of the budget
  edit_stale_turns: 4            # tool results in turns at least this old are stale
  edit_keep_recent_turns: 3      # the most recent N turns are never edited
```

Environment overrides: `NANO_CONTEXT_ENABLE_CONTEXT_EDITING`,
`NANO_CONTEXT_EDIT_TRIGGER_RATIO`, `NANO_CONTEXT_EDIT_STALE_TURNS`,
`NANO_CONTEXT_EDIT_KEEP_RECENT_TURNS`.

A turn is editable only when it satisfies *both* bounds (the more conservative
one wins): it must be at least `edit_stale_turns` old and outside the
`edit_keep_recent_turns` recent window.

## Artifact tracking

### Why

"Which files were modified?" is a known industry blind spot: in third-party
cross-evaluations every compaction method scored only **2.19–2.45 / 5** on
recalling file modifications after compression. LLM summaries simply cannot be
trusted to remember the session's file mutations — but the tool call records
already contain ground truth. Extracting it deterministically is a
differentiator.

### How it works

- `ExtractArtifactRecords` scans assistant tool calls for the
  filesystem-mutating tools — `write_file` (written), `edit_file` (modified),
  `delete_file` (deleted) — and merges them by path with last-write-wins
  semantics, sorted by path for byte-for-byte determinism. The data source is
  the tool call record, never LLM memory. (Free-form shell commands cannot be
  attributed deterministically and are out of scope.)
- Whenever compaction produces a summary message (`CompressMessages`), or the
  failure fallback drops history (`fallbackTruncate`), a manifest block is
  attached **outside** the LLM-summarized region:

  ```
  <artifact_manifest>
  # Files changed this session (deterministically extracted from tool call records; not part of the LLM summary).
  deleted | /tmp/old.go | delete_file
  modified | pkg/agent/turn.go | edit_file
  written | pkg/agent/context_editing.go | write_file
  </artifact_manifest>
  ```

- Manifests survive repeated compactions: extraction also parses any existing
  `<artifact_manifest>` block in the messages being compressed and merges it
  with newer tool call records, so artifact knowledge is never summarized
  away.

### Configuration

```yaml
context:
  enable_artifact_tracking: true  # default: true
```

Environment override: `NANO_CONTEXT_ENABLE_ARTIFACT_TRACKING`.

## Research basis

September 2026 harness-engineering field research: context editing at stable
boundaries preserves prompt-cache prefixes and is strictly cheaper than
compaction, so it should fire first; and deterministic artifact manifests fix
the weakest scored dimension of LLM summarization (file-mutation recall,
2.19–2.45/5 across all evaluated compaction methods).

## Code map

- `pkg/agent/context_editing.go` — `ContextEditor` (`ShouldEdit`, `EditContext`)
- `pkg/agent/artifact_tracking.go` — `ExtractArtifactRecords`,
  `FormatArtifactManifest`, `ParseArtifactManifest`
- `pkg/agent/context_compression.go` — manifest attachment in
  `CompressMessages` / `fallbackTruncate` (`appendArtifactManifest`)
- `pkg/agent/turn.go` — `Turn.maybeEditContext`, invoked before the
  compaction check in `requestOpenAIAPI`
- `pkg/config/config.go` — `ContextConfig` fields, defaults, env overrides
