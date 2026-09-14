# 上下文工程：上下文编辑与制品追踪

[English](./CONTEXT_ENGINEERING.md)

`pkg/agent` 中两个互补的上下文工程机制，依据 2026 年 9 月马具工程调研报告实现：

1. **上下文编辑**（`context_editing.go`）——与压缩（compaction）互补的轻量、
   确定性机制：随上下文逼近预算上限，自动清除陈旧的工具调用结果。
2. **制品追踪**（`artifact_tracking.go`）——确定性地记录本次会话中被
   写入/修改/删除的文件清单，附加到每次压缩结果中，保证压缩后 agent 仍能
   正确进行增量编辑。

## 上下文编辑

### 动机

压缩把整段历史替换为 LLM 生成的摘要：能力强大但昂贵（额外一次 LLM 调用）、
有损，并且会重写上下文前缀——使下游提示缓存失效。生产实证表明存在更便宜的
第一道防线：长会话中大部分 token 体积来自*陈旧的工具结果*（文件读取、搜索
命中、命令输出），模型已不再需要它们的原文。清除这些结果是确定性的、零成本
的，且往往单靠这一步就足够。

### 设计约束

- **只在稳定边界处编辑。** 编辑以完整*轮次*为单位（一条 user 消息加上直到
  下一条 user 消息之前的所有消息）。消息数量、顺序、角色绝不改变——只在原地
  替换陈旧 `tool` 消息的 `content`。轮次内部的部分编辑会不可预测地破坏提示
  缓存前缀，因此一轮要么整体编辑、要么保持原样。
- **幂等、确定性的占位符。** 被清除的结果变为
  `[tool result cleared: <tool_name>, <n> bytes]`。后续编辑会跳过已清除的
  结果，因此编辑过的前缀保持字节级一致，在提示缓存中保持命中。比占位符还短
  的结果绝不会被"放大"。
- **编辑先于压缩触发。** 编辑器在更低的利用率阈值触发（`edit_trigger_ratio`，
  默认 0.5），低于压缩阈值（通常 ≥0.7）。它在 `requestOpenAIAPI` 中的压缩
  检查之前由 `Turn.maybeEditContext` 执行；当编辑不足以把上下文降回预算内
  时，压缩作为兜底。

### 配置

```yaml
context:
  enable_context_editing: true   # 总开关（默认：true）
  edit_trigger_ratio: 0.5        # 使用量超过预算 50% 时触发编辑
  edit_stale_turns: 4            # 至少早于 N 轮的工具结果视为陈旧
  edit_keep_recent_turns: 3      # 最近 N 轮永不编辑
```

环境变量覆盖：`NANO_CONTEXT_ENABLE_CONTEXT_EDITING`、
`NANO_CONTEXT_EDIT_TRIGGER_RATIO`、`NANO_CONTEXT_EDIT_STALE_TURNS`、
`NANO_CONTEXT_EDIT_KEEP_RECENT_TURNS`。

一轮只有在*同时*满足两个边界时才可编辑（取更保守者）：既早于
`edit_stale_turns` 轮，又在 `edit_keep_recent_turns` 最近窗口之外。

## 制品追踪

### 动机

"哪些文件被修改过"是全行业盲区：在第三方横评中，所有压缩方法在压缩后回忆
文件修改这一项的得分仅为 **2.19–2.45 / 5**。LLM 摘要根本不可信来记住会话的
文件变更——但工具调用记录本身就是事实来源。确定性地提取它即是差异化能力。

### 工作方式

- `ExtractArtifactRecords` 扫描 assistant 消息中的工具调用，识别会修改文件
  系统的工具——`write_file`（written）、`edit_file`（modified）、
  `delete_file`（deleted）——按路径合并（后写胜出），并按路径排序以保证字节
  级确定性。数据来源是工具调用记录，绝不依赖 LLM 记忆。（自由形式的 shell
  命令无法确定性地归因，不在范围内。）
- 每当压缩生成摘要消息（`CompressMessages`），或失败兜底路径丢弃历史
  （`fallbackTruncate`）时，清单块会附加在 LLM 摘要范围**之外**：

  ```
  <artifact_manifest>
  # Files changed this session (deterministically extracted from tool call records; not part of the LLM summary).
  deleted | /tmp/old.go | delete_file
  modified | pkg/agent/turn.go | edit_file
  written | pkg/agent/context_editing.go | write_file
  </artifact_manifest>
  ```

- 清单在多次压缩间存活：提取时同时解析被压缩消息中已有的
  `<artifact_manifest>` 块并与新的工具调用记录合并，制品知识永远不会被
  摘要吞掉。

### 配置

```yaml
context:
  enable_artifact_tracking: true  # 默认：true
```

环境变量覆盖：`NANO_CONTEXT_ENABLE_ARTIFACT_TRACKING`。

## 调研依据

2026 年 9 月马具工程调研报告：在稳定边界处做上下文编辑可保持提示缓存前缀，
且严格比压缩便宜，因此应先触发；确定性的制品清单修复了 LLM 摘要评分最弱的
维度（文件修改回忆，所有受评压缩方法仅 2.19–2.45/5）。

## 代码位置

- `pkg/agent/context_editing.go` —— `ContextEditor`（`ShouldEdit`、`EditContext`）
- `pkg/agent/artifact_tracking.go` —— `ExtractArtifactRecords`、
  `FormatArtifactManifest`、`ParseArtifactManifest`
- `pkg/agent/context_compression.go` —— 在 `CompressMessages` /
  `fallbackTruncate` 中附加清单（`appendArtifactManifest`）
- `pkg/agent/turn.go` —— `Turn.maybeEditContext`，在 `requestOpenAIAPI`
  的压缩检查之前调用
- `pkg/config/config.go` —— `ContextConfig` 字段、默认值、环境变量覆盖
