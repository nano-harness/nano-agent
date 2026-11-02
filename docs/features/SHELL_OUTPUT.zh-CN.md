# Shell 输出捕获与内联预算

[English](./SHELL_OUTPUT.md)

`run_shell_command` 使用两个相互独立的输出上限：绝对的**捕获上限**，以及一个小得多的、用于控制实际进入模型上下文内容的**内联预算**。超出预算的输出会落盘保存（spill-to-file），因此失控的命令永远不会撑爆会话上下文。

## 概述

- **捕获上限**（`shell.capture_output_max_bytes`，默认 16MB）：命令执行期间每路 stdout/stderr 保留的绝对最大值。超过后丢弃最旧的字节，并在结果中标记 `output_truncated`。
- **内联预算**（`shell.inline_output_max_bytes`，默认 64KB）：内联进模型上下文的 stdout+stderr 合计上限。当捕获输出超过预算时，完整输出写入落盘文件，上下文中只保留头/尾预览和文件路径。

这与 2026 年业界实践一致（例如 Claude Code 的 `bashOutputMaxChars`）：为每次工具输出设置一个小的稳定预算，可以显著降低每轮 token 成本，同时完整输出仍可从磁盘恢复。

## 配置

```yaml
shell:
  inline_output_max_bytes: 65536     # 默认 64KB；<= 0 时使用默认值
  capture_output_max_bytes: 16777216 # 默认 16MB；<= 0 时使用默认值
```

## 落盘行为

当捕获输出超过内联预算时：

1. 完整捕获输出写入 `<用户缓存目录>/nano-shell-output/shell-output-<时间戳>-<pid>.txt`（权限 `0600`；用户缓存目录不可用时回退到系统临时目录）。
2. 内联内容变为**头部预览（约占预算 80%）**和**尾部预览（约占 20%）**，预算在 stdout 与 stderr 之间按比例分配，中间以省略标记（`... [N bytes omitted] ...`）标注。
3. 追加提示行：`[output truncated: full output written to <path>, N bytes total]`。

工具结果的 metadata 包含：

| 键 | 含义 |
| --- | --- |
| `output_spilled` | 输出落盘时为 `true` |
| `output_file` | 落盘文件的绝对路径 |
| `total_bytes` | 预览前捕获的总字节数（stdout + stderr） |
| `inline_output_max_bytes` | 实际生效的内联预算 |
| `output_truncated` / `max_output_bytes` | 行为不变：命中 16MB 级捕获上限时设置 |

流式输出回调与后台任务（`is_background`、超时自动转后台）不受影响：内联预算仅作用于返回给模型的前台执行结果。
