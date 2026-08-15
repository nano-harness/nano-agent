# Sub-agent 模型路由

[English](./sub-agent-routing.md)

Sub-agent 复用主 agent 在 `.nano.yaml` 中的多 provider 路由配置：`providers`、顶层 `fallbacks` 以及 `model_routing`。

## 解析规则

- 未设置 sub-agent 的 `model`：继承主 agent 的主路由和 fallback 链。
- 仅设置 sub-agent 的 `model`：将该模型用作主路由，同时继承主 agent 的 fallback 链。
- 同时设置 sub-agent 的 `model` 和 `fallbacks`：使用 sub-agent 的模型和 fallback 列表作为完整的路由链。

Sub-agent 的 `model` 和 `fallbacks` 取值是 provider/模型引用，例如 `deepseek/deepseek-chat` 或 `openai/gpt-4.1`，通过主配置的 `providers` 块进行解析。

## 熔断器

Sub-agent 按 provider 和 base URL 与主 agent 共享熔断器实例。如果某个 agent 打开了某个 provider 端点的熔断器，所有 agent 都会更快地绕开该不健康端点。

## 子进程模式

子进程 teammate 通过隐藏的 `nano teammate` 命令启动，并从工作目录读取 `.nano.yaml`。provider 的继承取决于是否从正确的项目目录启动子进程。`--model` 标志仍可覆盖主模型；fallback 链从配置/profile 数据加载，而不是通过命令行标志传递。

## 迁移旧示例

不要在 sub-agent 定义中使用 `model_base_url` 或 `model_api_key`。请在顶层 `providers` 块中定义端点和凭证数据，然后在 `.nano/agents/*.md` 中以 `provider/model` 的形式引用模型。
