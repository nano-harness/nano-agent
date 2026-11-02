# Daemon 运行时

[English](./DAEMON_RUNTIME.md)

Daemon 通过 HTTP 和 WebSocket API 暴露公共的会话与事件状态。

## 会话流

Team-lead 会话通过以下接口流式传输：

```text
GET /api/v1/teams/sessions/{id}/stream
```

支持的 WebSocket 帧类型包括：

- `subscribe`
- `replay`
- `lead_input`
- `cancel`
- `tool_approval`
- `approve`
- `reject`
- `ping`

## Replay API

历史团队会话事件通过以下接口暴露：

```text
GET /api/v1/teams/sessions/{id}/events?since_seq=N
```

Daemon 从会话事件存储中返回带序号的事件。

## 审批流程

当 lead agent 需要审批时，它会发出一个 `waiting_for_user` 事件，并带有 `metadata.kind=tool_approval_request`。客户端可以使用 WebSocket 审批帧进行批准或拒绝。

## 解耦规则

Daemon 和 UI 代码应当消费公共的会话/事件状态。它们不应依赖 agent 回合内部的私有实现来完成渲染、回放、审批或取消。

## 兼容性

除非在 [Daemon API](DAEMON_API.md) 中明确标记为已废弃，现有 daemon 端点和帧名称保持兼容。

## 日志流

Daemon 将结构化日志写入 `daemon.log_file` 指定的文件
（默认：`~/.nano/daemon.log`）。CLI 提供两种读取模式：

- `nano daemon logs -n <N>` 一次性打印末尾 `N` 行。
- `nano daemon logs --follow`（别名 `-f`）随着内容追加持续流式输出，类似
  `tail -f`。follow 实现每 200 ms 轮询一次文件，并通过比较当前文件的
  inode（Unix）或其 size+mtime（其他平台）来检测 logrotate 风格的轮转。
  当检测到轮转时，tailer 会透明地重新打开文件，并从新文件的开头继续。

取消：`Ctrl+C`（SIGINT）会取消 follow 的 context；`tailFollow`
在 context 结束后返回，CLI 干净退出，不会泄漏文件句柄。

## Scribe 与 OSS 同步

会话事件由 `SessionScribe`
（[`pkg/daemon/session_scribe.go`](../../pkg/daemon/session_scribe.go)）持久化，
它以 JSON-Lines 记录追加写入 `~/.nano/sessions/<id>/events.jsonl`（在 macOS 上为
`/tmp/nano-agent/sessions/<id>/events.jsonl`）。

行为说明：

- 每个 scribe 拥有一个由互斥锁保护的缓冲 writer；写入由防抖同步定时器提交，
  因此高频事件可以批量落盘而不阻塞生产者。
- 当配置了 `daemon.scribe.oss_*` 时，scribe 还会使用
  `aliyun-oss-go-sdk` 将已完成轮转的日志上传到配置的 OSS bucket。
  上传是尽力而为的，异步执行；失败会记录日志，但绝不会阻塞本地持久化。
- `Close()` 会冲刷待写入内容，并在返回前触发最后一次上传。

## 调度器

调度器由 `Server` 装配，暴露在 `/api/v1/scheduler/*`
（参见 [`pkg/daemon/scheduler_handlers.go`](../../pkg/daemon/scheduler_handlers.go)）。
实现说明：

- 调度使用 `github.com/robfig/cron/v3` 进行 cron 表达式解析与分发。
- 任务可以在运行时通过 HTTP API 添加、列出、暂停、恢复和移除；内存中的
  cron 表与 `~/.nano/cron/` 下的持久化 routine 存储保持同步。
- Daemon 重启时，调度器会从同一存储重新加载已持久化的 routines，之前注册的
  任务会继续触发，无需手动重新注册。

## 连接管理器

`ConnectionManager`
（[`pkg/daemon/connection_manager.go`](../../pkg/daemon/connection_manager.go)）
负责管理每个 WebSocket 连接，并遵循 gorilla/websocket 的最佳实践：

- 所有写入都汇聚到单个 goroutine，通过消费一个 `WriteMessage` channel 完成；
  这是必须的，因为 gorilla/websocket 不允许多个并发写者。
- 心跳 goroutine 按配置间隔发送 ping 帧，并强制执行读超时，以便检测半开连接。
- 大于 `MaxMessageSize`（64 KB）的出站载荷会被切分为 `ChunkSize`（32 KB）
  的分片，客户端通过 `ChunkedMessage` 信封（`id`、`index`、`total`、`complete`）
  重新组装。
