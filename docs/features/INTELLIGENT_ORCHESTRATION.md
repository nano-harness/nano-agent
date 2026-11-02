# Intelligent Sub-Agent Orchestration System

[中文](./INTELLIGENT_ORCHESTRATION.zh-CN.md)

## Overview

nano-agent's intelligent sub-agent orchestration system is an advanced task dispatch and execution framework. It automatically recognizes sub-agent indicators in user requests and intelligently assigns tasks to the most suitable specialized agents. By reducing unnecessary LLM calls and optimizing task routing, the system significantly improves response speed and execution efficiency.

## Core Components

### 1. IntelligentSubAgentOrchestrator (Intelligent Sub-Agent Orchestrator)

The intelligent orchestrator is the core component of the system, responsible for:

- **Indicator detection**: Automatically detects sub-agent indicators in user input
- **Execution plan generation**: Creates optimal execution plans based on task content
- **Task assignment**: Breaks down complex tasks and assigns them to appropriate sub-agents

#### Main Methods

```go
// Detect whether the input contains sub-agent indicators
func (o *IntelligentSubAgentOrchestrator) HasSubAgentIndicators(input string) bool

// Create an execution plan
func (o *IntelligentSubAgentOrchestrator) CreateExecutionPlan(ctx context.Context, userInput string) (*OrchestrationPlan, error)
```

### 2. UnifiedAgentTool (Unified Agent Tool)

The unified agent tool integrates the intelligent orchestrator and provides:

- **Unified interface**: A consistent interface for all agent operations
- **Intelligent routing**: Automatically selects the execution path based on task characteristics
- **Fallback mechanism**: Reliable fallback options when orchestration fails

### 3. Optimized Main Agent Flow

The main agent's `ProcessStream` method has been optimized to implement:

- **Pre-check mechanism**: Checks for sub-agent indicators before calling the LLM
- **Direct routing**: Routes unambiguous tasks directly to the corresponding agent
- **Performance optimization**: Avoids unnecessary LLM calls

## Workflow

### 1. Request Processing Flow

```
User input → Indicator detection → Routing decision → Execution
     ↓                ↓                     ↓             ↓
"写代码" (write code) → detects "code" → routes to coder → executes the task
```

### 2. Intelligent Detection Mechanism

The system detects sub-agent indicators in the following ways:

1. **Explicit agent names**: Directly mentioning an agent name (e.g. "have coder help me...")
2. **Keyword matching**: Matching keywords defined in the `WhenToUse` field
3. **Pattern recognition**: Recognizing specific trigger patterns (e.g. "@coder", "use [writer]")

### 3. Execution Modes

The system supports three execution modes:

- **Main agent mode**: Used when no sub-agent indicator is detected
- **Single sub-agent mode**: Used when a request explicitly targets a single sub-agent
- **Multi sub-agent mode**: Used when multiple sub-agents need to collaborate

## Configuration Examples

### Sub-Agent Configuration

```yaml
sub_agents:
  - agent_name: "coder"
    system_prompt: "You are a coding assistant specialized in writing and debugging code."
    when_to_use: "code, programming, debug, implement, function, class, method"
    model: "gpt-4"
    enabled: true

  - agent_name: "writer"
    system_prompt: "You are a writing assistant specialized in creating documentation and content."
    when_to_use: "write, document, content, article, blog, documentation"
    model: "gpt-4"
    enabled: true
```

### Trigger Examples

The system now only supports explicit invocation. The following inputs trigger the corresponding sub-agents:

```bash
# Trigger the coder sub-agent - explicit invocation
"@coder 请帮我写一个Python函数"  # "@coder, please write a Python function for me"
"让coder实现这个功能"           # "Have coder implement this feature"
"[coder] debug这个代码问题"     # "[coder] debug this code issue"
"use coder to fix this bug"
"coder help me with this algorithm"

# Trigger the writer sub-agent - explicit invocation
"@writer 写一份API文档"         # "@writer, write an API document"
"让writer创建用户手册"          # "Have writer create a user manual"
"使用[writer]生成内容"          # "Use [writer] to generate content"
"use writer to create documentation"
"writer help me write a blog post"

# Do not trigger a sub-agent (use the main agent)
"今天天气怎么样？"               # "How's the weather today?"
"你好"                         # "Hello"
"请帮我写一个Python函数"        # No explicit invocation, uses the main agent
"debug这个代码问题"             # No explicit invocation, uses the main agent
"写一份API文档"                # No explicit invocation, uses the main agent
```

**Note**: Keyword matching has been removed from the system; only explicit invocation is supported. This means you must explicitly specify the sub-agent to use, otherwise the request is handled by the main agent.

## Performance Optimization

### 1. Pre-Check Mechanism

The system performs a local check before calling the LLM:

```go
// Check whether there are sub-agent indicators
hasSubAgentIndicators := len(triggeredAgents) > 0
if !hasSubAgentIndicators {
    // Use the main agent directly, avoiding an LLM call
    return a.processDirectly(ctx, userInput, onEvent)
}
```

### 2. Caching and Reuse

- **Configuration caching**: Sub-agent configurations are loaded and cached at startup
- **Pattern reuse**: Compiled regular expression patterns are reused
- **Connection pooling**: HTTP connection reuse reduces overhead

### 3. Concurrent Processing

Multi sub-agent tasks support concurrent execution:

```go
// Execute multiple sub-agents concurrently
for _, agentName := range agentNames {
    go func(name string) {
        defer wg.Done()
        err := a.processWithSubAgent(ctx, userInput, name, eventHandler)
        // Process the result...
    }(agentName)
}
```

## API Interfaces

### REST API

```bash
# Execute a task
POST /api/v1/sessions/sess_demo/execute
{
    "command": "请帮我写一个排序算法",
    "timeout": 60,
    "include_steps": true
}
```

### WebSocket API

```javascript
// Establish a connection
const ws = new WebSocket('ws://localhost:8080/api/v1/stream');

// Send a task
ws.send(JSON.stringify({
  command: '写一个快速排序算法',  // "Write a quicksort algorithm"
  session_id: 'sess_demo',
  timeout: 60
}));
```

## Monitoring and Diagnostics

### 1. Logging

The system provides detailed logging:

```
[INFO] No sub-agent indicators detected, processing with main agent directly
[INFO] Delegating to sub-agent via unified tool: coder
[DEBUG] Found sub-agent trigger (@pattern): coder
```

### 2. Performance Metrics

- **Response time**: Time from request to first response
- **Routing accuracy**: Proportion of requests correctly routed to the appropriate agent
- **Cache hit rate**: Hit rate of the configuration and pattern caches

### 3. Error Handling

The system provides multi-layered error handling:

- **Orchestration failure fallback**: Falls back to the traditional mode when intelligent orchestration fails
- **Agent unavailability handling**: Handling strategy when the target agent is unavailable
- **Timeout protection**: Prevents long-running tasks from blocking the system

## Best Practices

### 1. Sub-Agent Design

- **Specialization**: Each sub-agent should focus on a specific domain
- **Clear boundaries**: Clearly define the scope of responsibility of each agent
- **Keyword optimization**: Carefully design `WhenToUse` keywords to improve matching accuracy

### 2. Performance Optimization

- **Sensible configuration**: Configure the number of sub-agents based on actual needs
- **Resource management**: Monitor memory and CPU usage
- **Caching strategy**: Use caches appropriately to reduce repeated computation

### 3. Error Handling

- **Graceful degradation**: Ensure the system keeps working when some components fail
- **Logging**: Record detailed error information for debugging
- **Monitoring and alerting**: Set up appropriate monitoring and alerting mechanisms

## Troubleshooting

### Common Issues

1. **Sub-agent not triggered**
   - Check the `WhenToUse` keyword configuration
   - Verify the sub-agent is enabled
   - Check the detection information in the logs

2. **Performance issues**
   - Check for unnecessary LLM calls
   - Monitor sub-agent response times
   - Optimize the keyword matching logic

3. **Orchestration failures**
   - Review the orchestrator's error logs
   - Check the LLM connection status
   - Validate the configuration file format

### Debugging Tips

```bash
# Enable verbose logging
export NANO_VERBOSE=true

# Inspect the orchestration decision process
nano --debug "你的任务"  # "your task"

# Test a specific sub-agent
nano subagent serve --name coder
```

## Future Development

### Planned Features

1. **Learning capability**: Optimize routing decisions based on historical data
2. **Dynamic configuration**: Dynamically adjust sub-agent configuration at runtime
3. **Load balancing**: Intelligently distribute tasks to balance load
4. **A/B testing**: Support comparative testing of different orchestration strategies

### Extensibility

The system design supports:

- **Pluggable architecture**: Easy to add new sub-agent types
- **Distributed deployment**: Sub-agent deployment across nodes
- **Cloud native**: Support for containerized and Kubernetes deployment
- **API extension**: Rich API interfaces for third-party integration
