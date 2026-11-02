# 文件系统检查点（M2-4）

[English](./CHECKPOINTING.md)

`pkg/checkpoint` 提供对 agent 工作树的快照/恢复能力，使破坏性操作可以被回滚。
它是 nano-agent 中与 Claude Code 文件系统检查点功能对应的实现。

## 策略

管理器会自动选择一种策略：

1. **Git stash（首选）**——如果工作目录位于 git 仓库内，
   `git stash create` 会生成一个 stash 对象，同时捕获已跟踪和未跟踪的变更。
   快照可通过 SHA 寻址。
2. **文件复制（回退方案）**——当工作目录不是 git 仓库时，
   目录树会被复制到 `~/.nano/checkpoints/<id>/`。体积庞大的目录
   （`.git`、`node_modules`、`.nano`）会被跳过，以保持快照小巧。

## 配置

```yaml
checkpoint:
  enabled: true            # off by default
  auto_snapshot: true      # snapshot before high-risk tool invocations
  max_count: 50            # rolling history size
  max_size_mb: 1024        # cap total backup storage
  retention_days: 7        # discard older snapshots
  backup_root: ~/.nano/checkpoints
```

## 斜杠命令

| 命令                     | 行为                                            |
|--------------------------|-------------------------------------------------|
| `/checkpoint [reason]`   | 立即拍摄一次快照。                              |
| `/checkpoints`           | 列出快照，最新的在前。                          |
| `/restore <checkpoint>`  | 将工作树回退到指定的快照。                      |

在交互模式下，`Restore` 始终需要确认。

## 程序化使用

```go
mgr, err := checkpoint.NewFSManager(checkpoint.Options{
    WorkingDir: workdir,
})
cp, _ := mgr.Snapshot("before edit_file", "edit_file")
defer mgr.Restore(cp.ID) // when something blows up
```

## 注意事项

- 检查点不能替代版本控制。
- 使用 git-stash 策略进行恢复时采用 `git stash apply`，可能会出现
  需要用户手动解决的冲突。
- 保留策略在快照时强制执行；失败/不完整的快照会在下一次调用时
  被清理。
