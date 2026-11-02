# Nano-Agent Skills Documentation

[中文](./README.zh-CN.md)

This directory contains skill documents for nano-agent covering startup modes, configuration, and daemon integration.

## Included files

- `SKILL.md`: main guide for TUI, Binary, ACP, and Daemon modes.
- `references/daemon-api.md`: REST API and WebSocket reference.
- `references/config-reference.md`: configuration reference for all modes.

## Install from the source tree

```bash
# Install globally
mkdir -p ~/.nano
cp -r docs/skills ~/.nano/

# Or install for one project
mkdir -p .nano
cp -r docs/skills .nano/
```

Nano-agent loads skills from `.nano/skills/` in the current project and `~/.nano/skills/` in your home directory. No release package or external download is required.

## Usage

The `nano-startup-modes` skill is triggered when you ask about startup modes, configuration, daemon APIs, or related troubleshooting. You can also read the documents directly:

```bash
cat ~/.nano/skills/SKILL.md
cat ~/.nano/skills/references/daemon-api.md
cat ~/.nano/skills/references/config-reference.md
```

## Package structure

```text
skills/
├── SKILL.md
└── references/
    ├── daemon-api.md
    └── config-reference.md
```

## Contributing

Update the source documents in this directory and their Chinese translations together. Report issues at https://github.com/nano-harness/nano-agent/issues.
