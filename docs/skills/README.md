# Nano-Agent Skills Documentation

[中文](./README.zh-CN.md)

This directory contains skill documents for nano-agent that help the agent understand and assist with various startup modes and configurations.

## What's Included

- **SKILL.md**: Main skill document covering all four startup modes (TUI, Binary, ACP, Daemon)
- **references/daemon-api.md**: Complete REST API and WebSocket documentation for Daemon mode
- **references/config-reference.md**: Comprehensive configuration reference for all modes

## Installation

### From Release Package

Download the latest skill package from releases:

```bash
# Download from GitHub Releases
wget https://github.com/nano-harness/nano-agent/releases/latest/download/nano-skills.tar.gz

# Or from OSS CDN (faster in China)
wget https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/latest/nano-skills.tar.gz

# Extract to your project
tar -xzf nano-skills.tar.gz -C /path/to/your/project/.nano/

# Or extract to global nano directory
mkdir -p ~/.nano
tar -xzf nano-skills.tar.gz -C ~/.nano/
```

### Verify Download

```bash
# Verify SHA256 checksum
wget https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/latest/nano-skills.tar.gz.sha256
sha256sum -c nano-skills.tar.gz.sha256
```

## Usage

Once extracted, the nano-agent will automatically load skills from:

1. **Project skills**: `.nano/skills/` in your project directory
2. **Global skills**: `~/.nano/skills/` in your home directory

The agent will use these skills to:
- Assist with mode selection (TUI, Binary, ACP, Daemon)
- Guide configuration setup
- Troubleshoot common issues
- Explain advanced features

## Skill Triggering

The `nano-startup-modes` skill is automatically triggered when you:
- Ask about startup modes or how to run nano-agent
- Request help with configuration
- Encounter mode-specific issues
- Need information about daemon API or advanced settings

## Manual Reference

You can also read the skill documents directly:

```bash
# View main skill document
cat ~/.nano/skills/SKILL.md

# View daemon API reference
cat ~/.nano/skills/references/daemon-api.md

# View configuration reference
cat ~/.nano/skills/references/config-reference.md
```

## Building from Source

If you're building nano-agent from source, the skills are already included in `docs/skills/`:

```bash
# Copy to global location
mkdir -p ~/.nano
cp -r docs/skills ~/.nano/

# Or copy to project location
mkdir -p .nano
cp -r docs/skills .nano/
```

## Package Structure

```
skills/
├── SKILL.md                        # Main startup modes guide
└── references/
    ├── daemon-api.md              # Daemon API documentation
    └── config-reference.md        # Configuration reference
```

## Download Locations

**Latest version**:
- GitHub: `https://github.com/nano-harness/nano-agent/releases/latest/download/nano-skills.tar.gz`
- OSS CDN: `https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/latest/nano-skills.tar.gz`

**Specific version** (replace `v1.0.0` with desired version):
- GitHub: `https://github.com/nano-harness/nano-agent/releases/download/v1.0.0/nano-skills-v1.0.0.tar.gz`
- OSS CDN: `https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/releases/v1.0.0/nano-skills-v1.0.0.tar.gz`

## Updates

Skill documents are updated with each nano-agent release. To get the latest documentation:

```bash
# Download latest version
wget https://binary-releases.oss-cn-hangzhou.aliyuncs.com/nano/latest/nano-skills.tar.gz -O /tmp/nano-skills.tar.gz

# Backup existing skills (if any)
[ -d ~/.nano/skills ] && mv ~/.nano/skills ~/.nano/skills.backup

# Extract new version
tar -xzf /tmp/nano-skills.tar.gz -C ~/.nano/
```

## Contributing

If you find issues or have suggestions for improving the skill documents, please:
1. Open an issue at https://github.com/nano-harness/nano-agent/issues
2. Submit a PR with improvements to `docs/skills/`

## License

These skill documents are part of nano-agent and share the same license.
