# 沙箱 × 权限模式矩阵

[English](./sandbox-permission-matrix.md)

> `nano binary exec` 在所有沙箱/权限组合下的预期行为。
> 本矩阵是 nano-agent 测试和 symphony 的 `resolveSandboxAndPermission` 校验
> 的唯一权威依据。

---

## 矩阵

| 平台     | `--sandbox` | 权限模式           | 预期行为                                                       | 状态    |
|----------|-------------|--------------------|----------------------------------------------------------------|---------|
| darwin   | `on`        | `default`          | sandbox-exec 生效；对危险工具弹出提示                          | ✅ 通过 |
| darwin   | `on`        | `acceptEdits`      | sandbox-exec 生效；文件编辑自动批准                            | ✅ 通过 |
| darwin   | `on`        | `auto`             | sandbox-exec 生效；由 LLM 分类器决定是否批准                   | ✅ 通过 |
| darwin   | `on`        | `yolo`             | sandbox-exec 生效；所有工具自动批准                            | ✅ 通过 |
| darwin   | `on`        | `plan`             | sandbox-exec 生效；只读（不允许任何修改）                      | ✅ 通过 |
| darwin   | `off`       | `default`          | 无沙箱；对危险工具弹出提示                                     | ✅ 通过 |
| darwin   | `off`       | `acceptEdits`      | 无沙箱；文件编辑自动批准                                       | ✅ 通过 |
| darwin   | `off`       | `auto`             | 无沙箱；由 LLM 分类器决定是否批准                              | ✅ 通过 |
| darwin   | `off`       | `yolo`             | 无沙箱；所有工具自动批准                                       | ✅ 通过 |
| darwin   | `off`       | `plan`             | 无沙箱；只读（不允许任何修改）                                 | ✅ 通过 |
| darwin   | `auto`      | `default`          | 仅在嵌入式*时启用沙箱；对危险工具弹出提示                      | ✅ 通过 |
| darwin   | `auto`      | `yolo`             | 仅在嵌入式*时启用沙箱；所有工具自动批准                        | ✅ 通过 |
| linux    | `on`        | `default`          | 原生沙箱（seccomp/namespace）；对危险工具弹出提示              | ✅ 通过 |
| linux    | `on`        | `acceptEdits`      | 原生沙箱；文件编辑自动批准                                     | ✅ 通过 |
| linux    | `on`        | `auto`             | 原生沙箱；由 LLM 分类器决定是否批准                            | ✅ 通过 |
| linux    | `on`        | `yolo`             | 原生沙箱；所有工具自动批准                                     | ✅ 通过 |
| linux    | `on`        | `plan`             | 原生沙箱；只读（不允许任何修改）                               | ✅ 通过 |
| linux    | `off`       | `default`          | 无沙箱；对危险工具弹出提示                                     | ✅ 通过 |
| linux    | `off`       | `acceptEdits`      | 无沙箱；文件编辑自动批准                                       | ✅ 通过 |
| linux    | `off`       | `auto`             | 无沙箱；由 LLM 分类器决定是否批准                              | ✅ 通过 |
| linux    | `off`       | `yolo`             | 无沙箱；所有工具自动批准                                       | ✅ 通过 |
| linux    | `off`       | `plan`             | 无沙箱；只读（不允许任何修改）                                 | ✅ 通过 |
| linux    | `auto`      | `default`          | 仅在嵌入式*时启用沙箱；对危险工具弹出提示                      | ✅ 通过 |
| linux    | `auto`      | `yolo`             | 仅在嵌入式*时启用沙箱；所有工具自动批准                        | ✅ 通过 |

*"嵌入式"（由 orchestrator 启动）= 设置了 `SYMPHONY_WORKSPACE`、`SYMPHONY_MCP_URL` 或 `NANO_ORCHESTRATOR_PROFILE` 环境变量。

---

## 不变量（适用于所有组合）

对于矩阵中的**每一个**单元格，以下各项必须成立：

1. **进程干净退出** —— 退出码为 `{0, 1, 10, 20, 30}` 之一。
2. **stdout 最后一行是合法 JSON** —— 可解析为 `binaryResultSummary`。
3. **`<output-dir>/result.json` 存在** —— 与 stdout JSON 逐字节一致。
4. **`<output-dir>/solution.patch`** —— 要么不存在（无变更），要么是合法的 unified diff。
5. **权限模式得到遵守** —— `plan` 模式不产生任何文件系统修改；`yolo` 不产生任何权限提示。

---

## 权限模式语义

| 模式          | 文件编辑 | Shell 命令 | 危险操作 | 备注                              |
|---------------|----------|------------|----------|-----------------------------------|
| `default`     | 提示     | 提示       | 提示     | 需要交互式确认                    |
| `acceptEdits` | 自动     | 提示       | 提示     | 仅文件写入工具自动批准            |
| `auto`        | 分类判定 | 分类判定   | 分类判定 | LLM 分类器风险评估                |
| `yolo`        | 自动     | 自动       | 自动     | 一切自动批准                      |
| `plan`        | 拒绝     | 拒绝       | 拒绝     | 只读；不允许修改                  |

---

## 沙箱后端行为

| 后端      | 平台            | 机制                                   |
|-----------|-----------------|----------------------------------------|
| `native`  | darwin          | 使用自定义 profile 的 `sandbox-exec`   |
| `native`  | linux           | seccomp + mount namespace              |
| `docker`  | darwin / linux  | 一次性容器隔离                         |
| （无）    | 任意            | 无隔离（沙箱禁用）                     |

---

## 模式组合的副作用

| 组合                                  | 副作用                                         |
|---------------------------------------|------------------------------------------------|
| `yolo` + 未配置沙箱后端               | 自动设置 `sandbox.backend = "docker"`          |
| `auto` + 无 `permission_auto` 配置    | 发出警告；回退到 `default` 行为                |
| `auto` + `ConfirmPolicy=allow`        | 被覆盖为 `block`，以实现失败即关闭（fail-closed） |

---

## 环境变量交互

当 `--sandbox` flag 与 `NANO_SANDBOX_*` 环境变量同时存在时：

```
--sandbox=off  →  sandbox disabled (env vars ignored for enabled state)
--sandbox=on   →  sandbox enabled; NANO_SANDBOX_BACKEND/NETWORK_ACCESS still apply for sub-settings
--sandbox=auto →  sandbox enabled only if embedded; env vars apply for sub-settings
```

`NANO_SANDBOX_ENABLED` 环境变量在配置加载时（`pkg/config`）应用，
但 `applyBinarySandboxMode()` 之后可以覆盖 `Enabled` 字段。这
意味着 `--sandbox=off` **始终优先**于 `NANO_SANDBOX_ENABLED=true`。

---

## CI 校验

在 CI 中校验本矩阵：

```bash
# Minimal smoke test: verify stdout JSON and exit code
nano binary exec --output-dir=/tmp/test-out --sandbox=off --permission-mode=yolo "echo hello"
jq . /tmp/test-out/result.json  # must succeed (valid JSON)

# Verify solution.patch format (if exists)
if [ -f /tmp/test-out/solution.patch ]; then
  # Must be valid unified diff or empty
  head -1 /tmp/test-out/solution.patch | grep -qE '^(diff |---|\+\+\+|$)'
fi
```

完整的矩阵 CI 运行应在 darwin 和 linux 两种 runner 上
遍历 `{on,off} × {default,acceptEdits,auto,yolo,plan}` 的所有组合。
