# Filesystem Checkpointing (M2-4)

[中文](./CHECKPOINTING.zh-CN.md)

`pkg/checkpoint` provides snapshot/restore of the agent's working tree so
destructive operations can be rolled back. It is the nano-agent counterpart of
the Claude Code filesystem checkpointing feature.

## Strategies

The manager selects a strategy automatically:

1. **Git stash (preferred)** — if the working dir is inside a git repo,
   `git stash create` produces a stash object that captures both tracked and
   untracked changes. Snapshots are addressable by SHA.
2. **File copy (fallback)** — when the working dir is not a git repo, the
   tree is copied into `~/.nano/checkpoints/<id>/`. Heavy directories
   (`.git`, `node_modules`, `.nano`) are skipped to keep snapshots small.

## Configuration

```yaml
checkpoint:
  enabled: true            # off by default
  auto_snapshot: true      # snapshot before high-risk tool invocations
  max_count: 50            # rolling history size
  max_size_mb: 1024        # cap total backup storage
  retention_days: 7        # discard older snapshots
  backup_root: ~/.nano/checkpoints
```

## Slash commands

| Command                  | Behaviour                                       |
|--------------------------|-------------------------------------------------|
| `/checkpoint [reason]`   | Take a snapshot now.                            |
| `/checkpoints`           | List snapshots, newest first.                   |
| `/restore <checkpoint>`  | Revert the working tree to the named snapshot.  |

`Restore` always requires confirmation in interactive mode.

## Programmatic use

```go
mgr, err := checkpoint.NewFSManager(checkpoint.Options{
    WorkingDir: workdir,
})
cp, _ := mgr.Snapshot("before edit_file", "edit_file")
defer mgr.Restore(cp.ID) // when something blows up
```

## Caveats

- Checkpoints are not a substitute for version control.
- Restore from the git-stash strategy uses `git stash apply` and may surface
  conflicts the user must resolve.
- Retention is enforced at snapshot time; failed/partial snapshots are
  cleaned up on the next call.
