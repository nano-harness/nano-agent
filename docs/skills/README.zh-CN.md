# Nano-Agent Skills 文档

[English](./README.md)

本目录包含 nano-agent 的 skill 文档，覆盖启动模式、配置和 Daemon 集成。

## 包含文件

- `SKILL.md`：TUI、Binary、ACP 和 Daemon 模式主指南。
- `references/daemon-api.md`：REST API 与 WebSocket 参考。
- `references/config-reference.md`：所有模式的配置参考。

## 从源码目录安装

```bash
# 安装到全局目录
mkdir -p ~/.nano
cp -r docs/skills ~/.nano/

# 或安装到单个项目
mkdir -p .nano
cp -r docs/skills .nano/
```

nano-agent 会加载当前项目 `.nano/skills/` 和用户主目录 `~/.nano/skills/` 中的 skills，无需发布包或外部下载。

## 使用方法

询问启动模式、配置、Daemon API 或相关问题时，`nano-startup-modes` skill 会触发。也可以直接阅读：

```bash
cat ~/.nano/skills/SKILL.md
cat ~/.nano/skills/references/daemon-api.md
cat ~/.nano/skills/references/config-reference.md
```

## 目录结构

```text
skills/
├── SKILL.md
└── references/
    ├── daemon-api.md
    └── config-reference.md
```

## 贡献

修改本目录中的英文文档时，请同步更新中文翻译。问题请提交到 https://github.com/nano-harness/nano-agent/issues。
