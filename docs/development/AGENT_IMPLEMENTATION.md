# nano-agent Current Agent Implementation Logic

[中文](./AGENT_IMPLEMENTATION.zh-CN.md)

This document focuses on the main implementation under `pkg/agent` in the current repository, explaining how a request flows from `Agent` initialization all the way through LLM calls, tool execution, context compression, session management, and subagent dispatch.

## 1. Entry Points and Core Objects

The main entry points of the current agent are:

- `pkg/agent/agent.go`
- `pkg/agent/turn.go`
- `pkg/agent/system_prompt.go`
- `pkg/agent/tool_scheduler.go`
- `pkg/agent/session.go`
- `pkg/agent/context_compression.go`

The two most central objects are:

1. `Agent`: responsible for initializing the runtime environment, maintaining the toolbox / llm client / session manager / sub-agent configuration, and serving as the entry point for external requests.
2. `Turn`: responsible for one concrete execution turn, internally containing the LLM call loop, tool invocation, termination-condition checks, and context compression logic.

---

## 2. Agent Initialization Flow

`New(cfg, approvalHandler)` in `pkg/agent/agent.go` performs the initialization of the main agent.

### 2.1 What Initialization Does

1. Reads the current working directory and builds a `tools.ToolboxConfig`
2. Creates the `sandbox.Middleware` (if enabled in the configuration)
3. Creates the `tools.Toolbox`
4. Initializes the `llm.Client` using `toolbox.List()`
5. Starts a goroutine that listens for MCP tool updates and syncs them via `llmClient.UpdateTools(...)`
6. Creates the `memory.Manager`
7. Creates the `ToolRecoveryStrategy` and the `ToolScheduler`
8. Creates the `SessionManager`
9. Registers the main agent, the static subagents from the configuration, and the `unified_agent` tool
10. If the current agent is not a subagent, also registers the `spawn_sub_agents` tool
11. If MCP is enabled, starts the MCP client asynchronously

### 2.2 Key Members After Initialization

The most important fields on an `Agent` instance:

- `toolbox`: the unified tool registry
- `llmClient`: handles streaming LLM calls
- `toolScheduler`: handles parallel tool scheduling, approval, retries, and status events
- `memoryManager`: entry point for memory management
- `sessionManager`: multi-session conversation isolation
- `subAgents`: static subagent definitions from the config file
- `loopDetector`: used for loop detection within a turn

---

## 3. Main Request-Handling Path

The commonly used external entry points all eventually funnel into:

- `ProcessStream(...)`
- `ProcessStreamWithMultimodal(...)`
- `ProcessStreamWithMultimodalAndSession(...)`

The true core is `ProcessStreamWithMultimodalAndSession()`.

### 3.1 Session First

In `pkg/agent/agent.go`:

1. First, get or create the session via `sessionManager.GetOrCreateSession(sessionID)`
2. Send the `session_info` event
3. Then call `processStreamWithSessionInternal(...)`

This means nano-agent's multi-turn conversation context is not attached directly to the `Agent`, but to the `Session`.

### 3.2 Responsibilities of processStreamWithSessionInternal

This method is the main routing function. It mainly does five things:

1. Parses slash commands and temporarily restricts the tools allowed in this turn according to the command definition
2. Detects whether a subagent was explicitly triggered, or whether the subagent selector should be consulted
3. If a subagent is matched, goes through the unified agent tool or falls back to the legacy subagent execution path
4. If no subagent is matched, continues with the main agent's turn execution
5. Retrieves the history messages from the current session, builds a `TurnConfig`, then creates and executes a `Turn`

---

## 4. Subagent Routing Logic

### 4.1 Trigger Detection

The current main logic has two layers:

1. `detectTriggeredSubAgents(userInput)`
   - Parses explicit trigger patterns, e.g. `@agentName`, `使用[agentName]`, `with:agentName`
2. `shouldUseSubAgent(userInput)`
   - Calls `subAgentSelector.SelectSubAgent(...)`
   - Used for non-explicit but policy-driven subagent selection

If neither layer matches, execution simply continues with the main agent, without asking the LLM to make an extra orchestration decision first.

### 4.2 Execution Path After a Subagent Match

If the `unified_agent` tool is registered in the toolbox, the preferred path is:

- `processWithUnifiedTool(...)`

Otherwise it falls back to the legacy paths:

- Single subagent: `processWithSubAgent(...)`
- Multiple subagents: `processWithMultipleSubAgents(...)`

### 4.3 Multiple-Subagent Mode

`processWithMultipleSubAgents(...)` will:

1. Start multiple subagents concurrently
2. Prefix each subagent's output
3. Collect the valid output from all subagents
4. After all subagents finish, have the main agent produce a unified aggregated summary

So it is essentially a "concurrent execution + main-agent aggregation" pattern.

### 4.4 Single-Subagent Mode

`processWithSubAgent(...)` will:

1. Find the configuration entry by `agentName`
2. If the config has `UseIPC=true`, hand off to `processWithSubAgentIPC(...)`
3. Otherwise clone a copy of the configuration in the current process and create a short-lived child agent
4. Clear `SubAgents` to prevent recursive further delegation
5. Filter the tools visible to the subagent according to `AllowedTools`
6. If the memory feature is enabled, additionally register the memory tools
7. Re-invoke `subAgent.llmClient.UpdateTools(allowedTools)`
8. Finally, let this child agent run `ProcessStream(...)` itself

### 4.5 IPC Subagent Mode

`processWithSubAgentIPC(...)` is another level of isolation:

1. Starts a local temporary HTTP subprocess
2. Randomly assigns a local port and API key
3. Waits for the service to become ready via a health check
4. Then forwards the task to the subagent through the local IPC client

This mode is suitable for stronger isolation, but it also costs more.

### 4.6 Dynamic Subagents

Besides the static subagents from the config file, the current implementation also supports creating subagents dynamically at runtime:

- Tool entry point: `pkg/agent/spawn_subagents_tool.go`
- Runtime entity: `pkg/agent/dynamic_subagent.go`

The main agent registers `spawn_sub_agents` during its own initialization, but only for the top-level main agent, not for subagents.  
This way the main agent can create a `DynamicSubAgent` on the fly during execution, while subagents cannot recursively keep spawning new subagents indefinitely.

A `DynamicSubAgent` executes similarly to a static in-process subagent:

1. Inherits the parent agent's base configuration and toolbox
2. Uses the passed-in `systemPrompt` to customize the role
3. Clears `SubAgents`
4. Sets `IsSubAgent=true`
5. Creates a short-lived derived agent to execute the task

### 4.7 AgentProfile and team-lead teammate

The configurable multi-agent entry from PR 12 uses `.nano/agents` under the project directory:

- `pkg/agentprofile` is responsible for discovering `.nano/agents/*.yaml|*.yml|*.json|*.md`.
- Profile fields include `name`, `description`, `initial_prompt`, `permission_mode`, `allowed_tools`, `model`, `kind`, and `color`.
- `@agent-name <task>` is deprecated: custom agent profiles are now triggered via `/agent-name <task>` (the dispatcher rewrites it into guidance for a `spawn_teammate` call).
- `spawn_teammate` reads the profile of the same name, filling in defaults for `initial_prompt`, `permission_mode`, `kind`, and `color` when missing.
- The teammate runner copies the parent configuration and separately sets the permission mode declared in the profile for the child agent, avoiding modifying the parent agent's permissions.

Example:

```yaml
# .nano/agents/reviewer.yaml
description: Review code changes
initial_prompt: Review the requested patch and report risks.
permission_mode: acceptEdits
allowed_tools: [read_file, run_shell_command]
kind: in_process
color: "#00ff00"
```

---

## 5. Turn Execution Loop

When the main agent does not route to a subagent, it creates a `Turn` and calls `Turn.Execute(ctx)`.  
This part is the core closed loop of the current agent.

### 5.1 Turn Initialization

`NewTurnWithMultimodal(...)` packs the following into the turn:

- The current working directory
- The toolbox
- The llm client
- The memory manager
- The tool scheduler
- The history messages of the current session
- The system prompt builder `SystemPromptBuilder`
- The context compression strategy `CompressionStrategy`
- The completion conditions `CompletionCriteria`

### 5.2 Execute Main Loop

The main loop of `Turn.Execute()` is roughly:

1. Send planner / executor status events
2. Check whether the termination conditions are met
3. Call `requestOpenAIAPI(ctx)`
4. Get the LLM's text output and tool-call list
5. If there are no tool calls, check whether the protocol requiring a `task_done` call was violated
6. If there are tool calls, execute the tools in parallel
7. Append the tool results back into the message context
8. Check whether the task is complete
9. Loop into the next round

At the end it also attempts to save the session memory and closes the turn.

---

## 6. What Happens Before and After an LLM Call

The key steps inside `Turn.requestOpenAIAPI(ctx)` are quite clear:

1. `ensureSystemPrompt()`: ensures the system prompt has been inserted into the message list
2. `ensureUserMessage()`: ensures the current user input is appended only once
3. `ShouldCompress()`: compresses first if the context is approaching the threshold
4. `LLMClient.StreamCompletion(...)`: performs the streaming generation
5. Appends the assistant reply and tool calls back to `t.Messages`
6. Increments the current iteration counter

One key point here: **the system prompt, history messages, current user input, and tool-result messages all ultimately land on `t.Messages`, serving as the complete context for the next LLM call.**

---

## 7. How the System Prompt Is Assembled

The system prompt logic lives mainly in `pkg/agent/system_prompt.go`.

In the current implementation, `BuildEnhancedSystemPrompt(...)` is assembled from roughly these parts:

1. `BuildBaseSystemPrompt()`: base role description, working directory, git/sandbox environment info
2. User environment info: timezone, operating system, shell, editor, available programming tools
3. Tool catalog: tools listed by category, with parameter schemas, required fields, and example parameters
4. Subagent descriptions: currently available subagents, models, allowed tools, and system prompts
5. Execution policy: tool-call conventions and principles to follow during execution
6. Current goals: if the turn configuration carries goals, they are appended to the end of the prompt

So the current nano-agent prompt is not a fixed template, but a composite of "base template + environment info + tool inventory + subagent capabilities + current goals".

---

## 8. Tool Invocation and Parallel Scheduling

The core of tool scheduling is in `pkg/agent/tool_scheduler.go`.

### 8.1 How Tools Are Executed Inside a Turn

In `Turn.Execute()`, the tool calls returned by the model are first converted into `ToolToExecute`, and then uniformly go through:

- `executeToolCallsInParallel(...)`
- Which underneath actually calls `ToolScheduler.ExecuteParallel(...)`

### 8.2 What ToolScheduler Does

`ToolScheduler` is responsible for:

- Tool-call validation
- State transitions: `validating` → `scheduled` → `executing` → `success/error/cancelled`
- The approval flow (if an `approvalHandler` is configured)
- Concurrent execution
- Retry and recovery strategies
- Sending worker events upward

This means the turn itself is more like an "orchestrator", while the real tool lifecycle management is centralized in the scheduler.

### 8.3 How Tool Results Flow Back into the Context

After tool execution completes, `addToolResultsToContext(...)` will:

1. Wrap each result into a `role=tool` message
2. Append it to `t.Messages`
3. Record it in `t.ToolResults`
4. Record the execution history
5. If the tool is `task_done` and it succeeded, mark the task as complete

---

## 9. Context Compression Logic

Context compression lives in `pkg/agent/context_compression.go` and `pkg/agent/turn.go`.

### 9.1 When It Triggers

Before every LLM call, `requestOpenAIAPI()` first checks:

- `t.ShouldCompress()`

If token usage is approaching the threshold, it runs `CompressMessages(...)`.

### 9.2 Compression Strategy

The core idea of `CompressionStrategy` is:

1. Keep the system message
2. Keep the most recent N rounds of messages
3. Summarize-compress the earlier history
4. Avoid cutting a tool call / tool result chain in the middle as much as possible

After compression completes, a `CompressionInfo` is produced, and the pre/post-compression token counts, compression ratio, and summary content are reported through the event stream.

---

## 10. Session and History Message Management

Session logic is in `pkg/agent/session.go`.

### 10.1 What a Session Does

Each `Session` stores:

- `ConversationHistory`
- Creation time / last active time
- Token statistics and duration
- Metadata

Therefore multi-turn conversations are isolated per session, rather than all requests sharing one global context.

### 10.2 What SessionManager Does

`SessionManager` is responsible for:

- `GetOrCreateSession(...)`
- Session TTL management
- Periodically cleaning up expired sessions
- Persisting to local or OSS storage

### 10.3 History Sanitization

Before entering a turn, the message sequence is also cleaned up once, to avoid incomplete tool-call/tool-result sequences left in the history that would break the ordering of subsequent model calls.

---

## 11. How Configuration Affects the Agent

The config loading entry point is `LoadConfig(...)` in `pkg/config/config.go`.

The configuration items with the biggest impact on the agent include:

- `APIKey` / `BaseURL` / `Model`
- `SubAgents`
- `EnableMCP` / `MCP`
- `AllowedCommands` / `BlockedCommands`
- `AllowedEnvVars` / `BlockedEnvVars`
- `ToolRecovery`
- `Sandbox`
- `Memory`
- `Turn`
- `ContextConfig`

A few are especially critical:

1. `SubAgents`: determines whether static subagents and `unified_agent` are registered
2. `IsSubAgent`: prevents registering main-agent-only tools, such as `spawn_sub_agents`, on subagents
3. `ToolRecovery`: controls the default retry behavior after tool failures and per-tool policies
4. `ContextConfig`: determines the context compression threshold, retention ratio, and number of recent rounds to keep

### 11.1 Sandbox Design Addendum

The current sandbox implementation provides Linux `bwrap`, macOS `sandbox-exec`, and `PathChecker` path-level access control. The future sandbox refactoring should upgrade from a "shell command wrapper" to a unified Sandbox Runtime, with Docker as a higher-priority, stronger-isolation execution backend. See the full design in [Sandbox Design](./SANDBOX_DESIGN.md).

---

## 12. What Pattern the Current Implementation Boils Down To

In one sentence, the current nano-agent implementation logic is:

> **A turn-based agent architecture with `Agent` as the outer orchestrator, `Turn` as the per-turn execution core, `ToolScheduler` as the tool-execution hub, `SessionManager` as the multi-turn context isolation layer, and static/dynamic subagents extending its capability boundaries.**

Its distinctive features are:

- The main path is a turn-based loop, not a one-shot single-request execution
- Tool calls are model-driven and executed concurrently by the scheduler
- The system prompt is assembled dynamically
- Session/history is decoupled from turn/execution
- Subagents support both in-process cloning and IPC isolation
- Context compression happens before every LLM call, as part of the main execution chain

---

## 13. Recommended Reading Order

If you want to dive deeper into the source code, read in this order:

1. `pkg/agent/agent.go`
2. `pkg/agent/turn.go`
3. `pkg/agent/system_prompt.go`
4. `pkg/agent/tool_scheduler.go`
5. `pkg/agent/session.go`
6. `pkg/agent/context_compression.go`
7. `pkg/agent/unified_agent.go`
8. `pkg/agent/dynamic_subagent.go`

This way you get the main chain clear first, then move on to subagents and advanced capabilities.
