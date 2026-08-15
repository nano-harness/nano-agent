# Nano-Agent Skills 文档

[English](./README.md)

本目录包含 nano-agent 的 skill 文档，用于帮助 agent 理解并协助处理各种启动模式和配置。

## 包含内容

- **SKILL.md**：主 skill 文档，涵盖全部四种启动模式（TUI、Binary、ACP、Daemon）
- **references/daemon-api.md**：Daemon 模式的完整 REST API 与 WebSocket 文档
- **references/config-reference.md**：适用于所有模式的完整配置参考

## 安装

### 从发布包安装

从 releases 下载最新的 skill 包：

```bash
# 从 GitHub Releases 下载
wget https://github.com/nano-harness/nano-agent/releases/latest/download/nano-skills.tar.gz

# 或从 OSS CDN 下载（国内速度更快）
wget https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/latest/nano-skills.tar.gz

# 解压到你的项目中
tar -xzf nano-skills.tar.gz -C /path/to/your/project/.nano/

# 或解压到全局 nano 目录
mkdir -p ~/.nano
tar -xzf nano-skills.tar.gz -C ~/.nano/
```

### 校验下载文件

```bash
# 校验 SHA256 校验和
wget https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/latest/nano-skills.tar.gz.sha256
sha256sum -c nano-skills.tar.gz.sha256
```

## 使用方法

解压完成后，nano-agent 会自动从以下位置加载 skills：

1. **项目级 skills**：项目目录下的 `.nano/skills/`
2. **全局 skills**：用户主目录下的 `~/.nano/skills/`

agent 将使用这些 skills 来：
- 协助选择启动模式（TUI、Binary、ACP、Daemon）
- 指导配置设置
- 排查常见问题
- 讲解高级功能

## Skill 触发

当你进行以下操作时，`nano-startup-modes` skill 会被自动触发：
- 询问启动模式或如何运行 nano-agent
- 请求配置方面的帮助
- 遇到特定模式相关的问题
- 需要了解 daemon API 或高级设置的信息

## 手动查阅

你也可以直接阅读 skill 文档：

```bash
# 查看主 skill 文档
cat ~/.nano/skills/SKILL.md

# 查看 daemon API 参考
cat ~/.nano/skills/references/daemon-api.md

# 查看配置参考
cat ~/.nano/skills/references/config-reference.md
```

## 从源码构建

如果你是从源码构建 nano-agent，skills 已经包含在 `docs/skills/` 目录中：

```bash
# 复制到全局位置
mkdir -p ~/.nano
cp -r docs/skills ~/.nano/

# 或复制到项目位置
mkdir -p .nano
cp -r docs/skills .nano/
```

## 包结构

```
skills/
├── SKILL.md                        # 启动模式主指南
└── references/
    ├── daemon-api.md              # Daemon API 文档
    └── config-reference.md        # 配置参考
```

## 下载地址

**最新版本**：
- GitHub: `https://github.com/nano-harness/nano-agent/releases/latest/download/nano-skills.tar.gz`
- OSS CDN: `https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/latest/nano-skills.tar.gz`

**指定版本**（将 `v1.0.0` 替换为所需版本）：
- GitHub: `https://github.com/nano-harness/nano-agent/releases/download/v1.0.0/nano-skills-v1.0.0.tar.gz`
- OSS CDN: `https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/releases/v1.0.0/nano-skills-v1.0.0.tar.gz`

## 更新

skill 文档随 nano-agent 的每次发布一并更新。获取最新文档的方法：

```bash
# 下载最新版本
wget https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/latest/nano-skills.tar.gz -O /tmp/nano-skills.tar.gz

# 备份已有 skills（如果存在）
[ -d ~/.nano/skills ] && mv ~/.nano/skills ~/.nano/skills.backup

# 解压新版本
tar -xzf /tmp/nano-skills.tar.gz -C ~/.nano/
```

## 贡献

如果你发现问题或对改进 skill 文档有建议，请：
1. 在 https://github.com/nano-harness/nano-agent/issues 提交 issue
2. 提交针对 `docs/skills/` 改进的 PR

## 许可证

这些 skill 文档是 nano-agent 的一部分，与其共享同一许可证。
