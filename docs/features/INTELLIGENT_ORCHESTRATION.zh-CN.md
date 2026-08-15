# 智能子代理编排系统

[English](./INTELLIGENT_ORCHESTRATION.md)

## 概述

nano-agent 的智能子代理编排系统是一个先进的任务分发和执行框架，能够自动识别用户请求中的子代理标识符，并智能地将任务分配给最合适的专业化代理。该系统通过减少不必要的LLM调用和优化任务路由，显著提升了系统的响应速度和执行效率。

## 核心组件

### 1. IntelligentSubAgentOrchestrator (智能子代理编排器)

智能编排器是系统的核心组件，负责：

- **标识符检测**: 自动检测用户输入中的子代理标识符
- **执行计划生成**: 基于任务内容创建最优的执行计划
- **任务分配**: 将复杂任务分解并分配给合适的子代理

#### 主要方法

```go
// 检测输入是否包含子代理标识符
func (o *IntelligentSubAgentOrchestrator) HasSubAgentIndicators(input string) bool

// 创建执行计划
func (o *IntelligentSubAgentOrchestrator) CreateExecutionPlan(ctx context.Context, userInput string) (*OrchestrationPlan, error)
```

### 2. UnifiedAgentTool (统一代理工具)

统一代理工具集成了智能编排器，提供：

- **统一接口**: 为所有代理操作提供一致的接口
- **智能路由**: 根据任务特征自动选择执行路径
- **回退机制**: 在编排失败时提供可靠的回退选项

### 3. 优化的主代理流程

主代理的 `ProcessStream` 方法经过优化，实现：

- **预检查机制**: 在调用LLM之前先检查子代理标识符
- **直接路由**: 对于明确的任务直接路由到相应代理
- **性能优化**: 避免不必要的LLM调用

## 工作流程

### 1. 请求处理流程

```
用户输入 → 标识符检测 → 路由决策 → 执行
    ↓           ↓           ↓        ↓
  "写代码"   → 检测到"code" → 路由到coder → 执行任务
```

### 2. 智能检测机制

系统通过以下方式检测子代理标识符：

1. **显式代理名称**: 直接提及代理名称（如 "让coder帮我..."）
2. **关键词匹配**: 匹配 `WhenToUse` 字段中定义的关键词
3. **模式识别**: 识别特定的触发模式（如 "@coder", "使用[writer]"）

### 3. 执行模式

系统支持三种执行模式：

- **主代理模式**: 没有检测到子代理标识符时使用
- **单子代理模式**: 明确指向单个子代理时使用
- **多子代理模式**: 需要多个子代理协作时使用

## 配置示例

### 子代理配置

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

### 触发示例

系统现在只支持显式调用模式，以下输入会触发相应的子代理：

```bash
# 触发 coder 子代理 - 显式调用
"@coder 请帮我写一个Python函数"
"让coder实现这个功能"
"[coder] debug这个代码问题"
"use coder to fix this bug"
"coder help me with this algorithm"

# 触发 writer 子代理 - 显式调用
"@writer 写一份API文档"
"让writer创建用户手册"
"使用[writer]生成内容"
"use writer to create documentation"
"writer help me write a blog post"

# 不触发子代理（使用主代理）
"今天天气怎么样？"
"你好"
"请帮我写一个Python函数"  # 没有显式调用，使用主代理
"debug这个代码问题"        # 没有显式调用，使用主代理
"写一份API文档"           # 没有显式调用，使用主代理
```

**注意**: 系统已移除关键词匹配功能，只支持显式调用模式。这意味着必须明确指定要使用的子代理，否则将使用主代理处理请求。

## 性能优化

### 1. 预检查机制

系统在调用LLM之前先进行本地检查：

```go
// 检查是否有子代理标识符
hasSubAgentIndicators := len(triggeredAgents) > 0
if !hasSubAgentIndicators {
    // 直接使用主代理，避免LLM调用
    return a.processDirectly(ctx, userInput, onEvent)
}
```

### 2. 缓存和复用

- **配置缓存**: 子代理配置在启动时加载并缓存
- **模式复用**: 编译后的正则表达式模式被复用
- **连接池**: HTTP连接复用减少开销

### 3. 并发处理

多子代理任务支持并发执行：

```go
// 并发执行多个子代理
for _, agentName := range agentNames {
    go func(name string) {
        defer wg.Done()
        err := a.processWithSubAgent(ctx, userInput, name, eventHandler)
        // 处理结果...
    }(agentName)
}
```

## API 接口

### REST API

```bash
# 执行任务
POST /api/v1/sessions/sess_demo/execute
{
    "command": "请帮我写一个排序算法",
    "timeout": 60,
    "include_steps": true
}
```

### WebSocket API

```javascript
// 建立连接
const ws = new WebSocket('ws://localhost:8080/api/v1/stream');

// 发送任务
ws.send(JSON.stringify({
  command: '写一个快速排序算法',
  session_id: 'sess_demo',
  timeout: 60
}));
```

## 监控和诊断

### 1. 日志记录

系统提供详细的日志记录：

```
[INFO] No sub-agent indicators detected, processing with main agent directly
[INFO] Delegating to sub-agent via unified tool: coder
[DEBUG] Found sub-agent trigger (@pattern): coder
```

### 2. 性能指标

- **响应时间**: 从请求到首次响应的时间
- **路由准确性**: 正确路由到合适代理的比例
- **缓存命中率**: 配置和模式缓存的命中率

### 3. 错误处理

系统提供多层错误处理：

- **编排失败回退**: 智能编排失败时回退到传统模式
- **代理不可用处理**: 目标代理不可用时的处理策略
- **超时保护**: 防止长时间运行的任务阻塞系统

## 最佳实践

### 1. 子代理设计

- **专业化**: 每个子代理应专注于特定领域
- **清晰的边界**: 明确定义每个代理的职责范围
- **关键词优化**: 精心设计 `WhenToUse` 关键词以提高匹配准确性

### 2. 性能优化

- **合理配置**: 根据实际需求配置子代理数量
- **资源管理**: 监控内存和CPU使用情况
- **缓存策略**: 合理使用缓存减少重复计算

### 3. 错误处理

- **优雅降级**: 确保系统在部分组件失败时仍能工作
- **日志记录**: 详细记录错误信息以便调试
- **监控告警**: 设置适当的监控和告警机制

## 故障排除

### 常见问题

1. **子代理未被触发**
   - 检查 `WhenToUse` 关键词配置
   - 验证子代理是否启用
   - 查看日志中的检测信息

2. **性能问题**
   - 检查是否有不必要的LLM调用
   - 监控子代理的响应时间
   - 优化关键词匹配逻辑

3. **编排失败**
   - 查看编排器的错误日志
   - 检查LLM连接状态
   - 验证配置文件格式

### 调试技巧

```bash
# 启用详细日志
export NANO_VERBOSE=true

# 查看编排决策过程
nano --debug "你的任务"

# 测试特定子代理
nano subagent serve --name coder
```

## 未来发展

### 计划功能

1. **学习能力**: 基于历史数据优化路由决策
2. **动态配置**: 运行时动态调整子代理配置
3. **负载均衡**: 智能分配任务以平衡负载
4. **A/B测试**: 支持不同编排策略的对比测试

### 扩展性

系统设计支持：

- **插件化架构**: 易于添加新的子代理类型
- **分布式部署**: 支持跨节点的子代理部署
- **云原生**: 支持容器化和Kubernetes部署
- **API扩展**: 丰富的API接口支持第三方集成
