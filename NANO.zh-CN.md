# nano-agent LLM error handling

[English](./NANO.md)

## 重试、failback 与熔断器分类

`pkg/llm` 将提供商错误分类为相互独立的重试信号和 failback 信号。上下文溢出（context overflow）在 LLM 客户端层被有意地设置为不重试：agent 层在 `pkg/agent/turn_policy.go` 中处理它，先尝试消息压缩，然后使用同一模型重试。

| 错误类别 | 可重试 | ShouldFailback | 计入熔断器失败 | Agent 行为 |
|---|---:|---:|---:|---|
| RateLimit (429) | ✅ | ✅ | ✅ | 退避后使用同一模型重试；反复失败可能触发 failback |
| Server (5xx) | ✅ | ✅ | ✅ | 退避后使用同一模型重试；反复失败可能触发 failback |
| Timeout / Network | ✅ | ✅ | ✅ | 退避后使用同一模型重试；反复失败可能触发 failback |
| Authentication (401) | ❌ | ❌ | ❌ | 立即失败 |
| Quota (402/403) | ❌ | ❌ | ❌ | 立即失败 |
| ContextOverflow | ❌ | ❌ | ❌ | Agent 调用 `CompressMessages` 后重试 |
| Aborted (context canceled) | ❌ | ❌ | ❌ | 向上传播 `ctx.Err()` |
| OutputFormat | ❌ | ❌ | ❌ | Agent 可以修正 prompt/工具格式后重试 |

以 `finish_reason=length` 结束的流式响应，会在流事件元数据中暴露 `truncated=true` 和 `finish_reason=length`，使 agent 能够请求续写，而不会将部分输出静默地当作完整输出。

---

## 安全默认值（本次发布有变更）

### 沙箱（A2）

进程级沙箱现在**默认启用**。

| 平台 | 沙箱后端 | 效果 |
|---|---|---|
| macOS | `sandbox-exec` (seatbelt) | Shell 命令在 Apple Sandbox profile 下运行 |
| Linux | `bwrap` (bubblewrap) | Shell 命令在 bubblewrap 容器中运行 |
| 其他 | Noop（无隔离） | **启动时会打印醒目警告** |

**沙箱内的网络访问默认禁用。** 需要出站网络访问的工作流必须显式启用：

```yaml
# nano config file
sandbox:
  enabled: true         # default: true (was false)
  network_access: true  # default: false (was true)
```

或者通过环境变量：
```
NANO_SANDBOX_ENABLED=true
NANO_SANDBOX_NETWORK_ACCESS=true
```

要为特定运行恢复之前（安全性较低）的行为：
```
nano --sandbox=off ...
```

> **CI / 无头（headless）使用场景：** 如果你的 CI 流水线以非交互方式运行 nano-agent 且没有可用的 `bwrap`/`sandbox-exec`，请显式设置 `NANO_SANDBOX_ENABLED=false`。此时会出现警告，但 agent 会继续运行。对于安装了 bubblewrap 的 Linux CI，建议使用默认沙箱。
