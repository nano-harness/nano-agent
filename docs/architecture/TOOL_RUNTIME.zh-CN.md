# 工具运行时

[English](./TOOL_RUNTIME.md)

`pkg/toolruntime` 是工具元数据、目录查找与执行的稳定运行时接缝（seam）。

## 组件

- 元数据描述符用于描述工具的身份、类别、schema 以及执行能力。
- 目录（catalog）对外暴露规范化后的工具描述符。
- 运行时通过已注册的工具实现和中间件链来执行工具。

## 兼容性

现有的 `pkg/tools` 描述符 API 仍作为兼容性别名保留。通过 toolbox 代码进行的现有工具注册仍然受支持，而执行会委托给 `pkg/toolruntime.Runtime`。

## 执行预期

工具执行应当：

1. 按名称解析已注册的工具；
2. 在 schema 可用时通过工具 schema 校验参数；
3. 应用策略（policy）、沙箱（sandbox）、钩子（hook）和审计（audit）中间件；
4. 返回一个 `interfaces.ToolResult`；
5. 通过外围的 agent/session 运行时发出公共事件。

## 工具元数据预期

工具元数据应当包含：

- 稳定的名称；
- 描述；
- 类别；
- 参数 schema；
- 确认（confirmation）要求；
- 并发安全性。

工具元数据会被 CLI/TUI/daemon 等界面以及扩展清单（extension manifests）使用，因此应避免任何特定于 UI 的假设。
