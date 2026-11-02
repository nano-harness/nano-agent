# MCP 工具懒加载（工具搜索模式）

[English](./MCP_TOOL_SEARCH.md)

工具 schema 是一笔隐性的 token 税：每个已注册的工具定义都会随每次请求
发给模型。2026 年 9 月马具工程调研实测：仅 7 个以上 MCP 服务器配置就
吃掉约 6.7 万 token；Anthropic 的工具搜索报告也显示，大型工具库在把
schema 推迟到搜索元工具之后可减少约 85% 的 token 占用。

nano-agent 实现了**基于阈值的懒加载**：当 MCP 工具定义总量较小时，按
完整 schema 直接注入（零额外往返）；一旦超过上下文窗口中可配置的占比，
就切换到*工具搜索模式*。

## 工作方式

1. **评估**（`pkg/agent/tool_search.go`）——每当工具清单变化（内置工具
   注册、MCP 工具异步注册/注销），agent 估算每个 MCP 工具定义的 token
   成本（名称 + 描述 + JSON schema，约 4 字符/token），将总量与
   `threshold_ratio × context_window` 比较。
2. **积极模式（低于阈值）**——MCP 工具与核心工具一致：系统提示与 API
   工具列表中都带完整 schema。
3. **懒加载模式（超过阈值）**——不再全量注入 schema。模型只看到：
   - 轻量的 `discover_tools` 元工具；
   - 系统提示中的极简索引（工具名 + 一行描述，按服务器分组）。
4. **激活**——agent 通过 `discover_tools` 按关键词或精确名称检索，拿到
   完整 schema 后该工具在 `ProgressiveDisclosure` 门控中被标记为*已展开*，
   此后其 schema 对模型可见（受展开配额驱逐约束）。工具派发与 MCP 客户端
   完全不受影响——两种模式下执行路径一致。

每次工具清单变化都会重新评估，因此增删 MCP 服务器会自动切换模式。

## 配置

```yaml
tool_search:
  enabled: true          # 默认：true；false 强制积极模式
  threshold_ratio: 0.10  # 上下文窗口占比阈值（默认 0.10，
                         # 参照 Claude Code 约 10% 的行为）
  context_window: 0      # 覆盖阈值计算所用的窗口大小；
                         # 默认取 context.model_context_window，否则 200000
```

## 与现有机制的关系

- 门控层复用已有的 `ProgressiveDisclosure` 索引与 `discover_tools` 元工具；
  `ToolSearchGate` 只在其上增加阈值驱动的积极/懒加载开关。
- 工具调度器的 schema 自动注入路径（先取 schema 再重试调用）在懒加载
  模式下继续有效。
- 子 agent 不受影响（不注册管理类工具）。

## 实现说明

- `EvaluateToolSearch` 与 `EstimateToolDefinitionTokens` 为纯函数，与门控
  的暴露/隐藏矩阵、系统提示渲染开关一起由表驱动测试覆盖。
- 懒加载开关翻转时会使系统提示缓存失效，下一轮即渲染正确的工具段落。
