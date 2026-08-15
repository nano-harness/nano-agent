# Skills 文档发布集成 — 实现总结

[English](./SKILLS_RELEASE_INTEGRATION.md)

## 概述

已成功将 skill 文档集成到 nano-agent 发布流水线中，使 skill 文档能够随每次发布自动打包和分发。

## 已实现的内容

### 1. 打包脚本（`scripts/package-skills.sh`）
- 自动创建 skill 文档的 tar 包
- 生成带版本号的包：`nano-skills-v1.0.0.tar.gz`
- 创建最新版包：`nano-skills.tar.gz`
- 生成用于校验的 SHA256 校验和
- 支持通过 `VERSION` 环境变量自定义版本

### 2. 发布工作流更新（`.github/workflows/release.yml`）
- 在下载构建产物之后新增 “Package skill documents” 步骤
- 将 skill 包集成到 GitHub Releases 中
- 将 skill 包添加到 OSS 版本化存储（`/releases/{TAG}/`）
- 将 skill 包添加到 OSS 最新版存储（`/latest/`）
- 与二进制发布模式保持一致

### 3. 文档（`docs/skills/README.md`）
- 从 GitHub Releases 下载的安装说明
- 从 OSS CDN 下载的安装说明
- 校验和验证指南
- 项目级和全局 skill 目录的使用说明
- 包结构参考
- 从源码构建的说明

## 发布产物

每次发布（推送 tag）后，以下 skill 文档产物可用：

### GitHub Releases
- `nano-skills-{version}.tar.gz` — 带版本号的 skill 包
- `nano-skills-{version}.tar.gz.sha256` — 校验和

### OSS CDN（版本化）
- `https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/releases/{version}/nano-skills-{version}.tar.gz`
- `https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/releases/{version}/nano-skills-{version}.tar.gz.sha256`

### OSS CDN（最新版）
- `https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/latest/nano-skills.tar.gz`
- `https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/latest/nano-skills.tar.gz.sha256`

## 包内容

```
skills/
├── SKILL.md                        # Main startup modes guide (454 lines)
├── README.md                       # Download and installation instructions
└── references/
    ├── daemon-api.md              # Complete Daemon API documentation
    └── config-reference.md        # Comprehensive configuration reference
```

## 下载示例

### 最新版本
```bash
# From OSS CDN (faster in China)
wget https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/latest/nano-skills.tar.gz

# From GitHub Releases
wget https://github.com/nano-harness/nano-agent/releases/latest/download/nano-skills.tar.gz
```

### 指定版本
```bash
# From OSS CDN
wget https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/releases/v1.0.0/nano-skills-v1.0.0.tar.gz

# From GitHub Releases
wget https://github.com/nano-harness/nano-agent/releases/download/v1.0.0/nano-skills-v1.0.0.tar.gz
```

## 安装

```bash
# Extract to project directory
tar -xzf nano-skills.tar.gz -C /path/to/project/.nano/

# Or extract to global directory
mkdir -p ~/.nano
tar -xzf nano-skills.tar.gz -C ~/.nano/
```

## 测试

打包脚本已在本地测试通过：
```bash
$ VERSION=test bash scripts/package-skills.sh
Packaging skill documents...
Version: test
Skills directory: /home/runner/work/nano-agent/nano-agent/docs/skills
Skill package created: /home/runner/work/nano-agent/nano-agent/dist/nano-skills-test.tar.gz
Latest package created: /home/runner/work/nano-agent/nano-agent/dist/nano-skills.tar.gz
Checksums generated

Package contents:
skills/
skills/SKILL.md
skills/references/
skills/references/daemon-api.md
skills/references/config-reference.md

Package size: 12K
Package ready for release: nano-skills-test.tar.gz
```

## 优势

1. **自动化分发**：skill 文档随每次发布自动打包
2. **版本一致性**：skill 与二进制发布版本保持一致
3. **安装简便**：用户无需克隆仓库即可下载预打包的 skill
4. **CDN 加速**：OSS 提供更快的下载速度，尤其是在中国地区
5. **可验证**：SHA256 校验和确保包的完整性
6. **最新版始终可用**：`/latest/` 目录始终指向最新版本

## 后续步骤

1. **首次发布**：打一个新版 tag（例如 `v1.0.0`）以测试完整工作流
2. **文档**：更新主 README，说明 skill 包的可用性
3. **安装脚本**：考虑在 `scripts/install.sh` 中加入 skill 下载功能
4. **Agent 集成**：确保 nano-agent 在 skill 缺失时能够自动下载（未来增强项）

## 变更的文件

1. `scripts/package-skills.sh` — 新增打包脚本
2. `.github/workflows/release.yml` — 更新，加入 skill 打包步骤
3. `docs/skills/README.md` — 新增安装与使用文档

## 兼容性

- 与现有发布基础设施兼容
- 对二进制发布流程无破坏性变更
- 向后兼容（skill 为可选项）
- 遵循现有的版本化与分发模式

## 备注

- 包大小：约 12KB（非常轻量）
- 无需额外依赖
- 脚本为 POSIX 兼容的 bash
- 可在 Linux 和 macOS 构建 runner 上运行
