# SWE-bench Evaluation for Nano-Agent

[English](./README.md)

这是一个优化的SWE-bench评估脚本，支持使用预构建的Docker镜像和二进制模式执行nano-agent任务。

## 主要特性

- 🐳 **预构建镜像支持**: 使用现成的Docker镜像，无需从头构建
- ⚡ **二进制模式**: 支持nano-agent二进制模式执行
- 📝 **YAML配置**: 灵活的配置文件模板
- 🔧 **多种运行模式**: 支持完整评估、仅拉取镜像、生成配置模板等
- 📊 **详细统计**: 提供详细的执行统计和结果报告
- 🏗️ **自动环境设置**: 自动创建Python虚拟环境和安装依赖
- 🔧 **模块化设计**: 基于类的设计，易于扩展和维护

## 快速开始

### 前置要求

- Python 3.8+
- Docker
- Git
- 已构建的nano-agent二进制文件

### 1. 基本用法

```bash
# 运行完整的SWE-bench评估
python run_swe_bench.py --test-instances astropy__astropy-13236

# 使用自定义配置文件
python run_swe_bench.py --test-instances astropy__astropy-13236 --config-template config_templates/swebench_config.yaml
```

### 2. 仅拉取Docker镜像

```bash
# 预先拉取所有需要的Docker镜像
python run_swe_bench.py --test-instances astropy__astropy-13236 django__django-12345 --pull-images-only
```

### 3. 生成配置模板

```bash
# 生成YAML配置模板
python run_swe_bench.py --generate-template my_config.yaml
```

### 4. 跳过构建步骤

```bash
# 如果已经有nano二进制文件，跳过构建步骤
python run_swe_bench.py --test-instances astropy__astropy-13236 --skip-build
```

脚本会自动：
1. 创建Python虚拟环境（如果不存在）
2. 安装必要的依赖包
3. 克隆SWE-bench仓库（如果不存在）
4. 拉取预构建的Docker镜像
5. 构建nano-agent（除非使用--skip-build）
6. 在指定实例上运行评估
7. 生成结果报告

## 命令行参数

| 参数 | 描述 | 默认值 |
|------|------|--------|
| `--test-instances` | 要测试的实例ID列表 | 必需 |
| `--results-dir` | 结果保存目录 | `./swe_bench_results` |
| `--swe-bench-path` | SWE-bench仓库路径 | `./SWE-bench` |
| `--nano-binary` | nano-agent二进制文件路径 | `../target/release/nano` |
| `--config-template` | YAML配置模板路径 | `config_templates/swebench_config.yaml` |
| `--pull-images-only` | 仅拉取Docker镜像 | False |
| `--generate-template` | 生成配置模板到指定路径 | None |
| `--skip-build` | 跳过nano-agent构建步骤 | False |
| `--parallel` | 启用并行执行（实验性） | False |

## Docker镜像

脚本使用预构建的Docker镜像，格式为：
```
ghcr.nju.edu.cn/epoch-research/swe-bench.eval.x86_64.{instance_id}
```

例如：
```bash
docker pull ghcr.nju.edu.cn/epoch-research/swe-bench.eval.x86_64.astropy__astropy-13236
```

## 配置文件

### 配置模板 (swebench_config.yaml)

项目提供了一个统一的配置模板文件，包含了 SWE-bench 评估所需的所有配置选项：

```yaml
# Core LLM Configuration
api_key: "${ANTHROPIC_API_KEY}"
base_url: "https://api.anthropic.com/v1"
model: "claude-3-5-sonnet-20241022"
verbose: true

# Performance Settings
max_context_files: 50
tree_depth: 3
response_timeout: 1800  # seconds
max_response_size: 10485760  # 10MB

# Context Management
context:
  max_tokens: 200000  # 适合 SWE-bench 的大上下文
  compression_ratio: 0.25
  preserve_recent_turns: 6
  enable_compression: true

# Tool Configuration
enabled_tools: ["filesystem", "search", "system"]

# Custom Configuration for SWE-bench
custom_config:
  swe_bench_mode: true
  working_directory: "/testbed"  # SWE-bench standard working directory
  output_directory: "/testbed/output"
  patch_file: "solution.patch"
```

配置文件与 nano-agent 的标准配置格式完全一致，支持所有 nano-agent 的功能特性。

## 环境要求

- Python 3.8+
- Docker
- 必需的Python包：
  ```bash
  pip install pyyaml
  ```

## 环境变量

设置必要的API密钥：

```bash
export ANTHROPIC_API_KEY="your-anthropic-api-key"
# 或其他LLM提供商的API密钥
```

### 性能分析（pprof）

在 SWE-bench 容器中以二进制模式运行 nano-agent 时，脚本会通过环境变量自动启用 pprof：

- `NANO_ENABLE_PPROF=true`
- `NANO_PPROF_PORT=6060`（默认）

容器内访问（仅绑定到本机 127.0.0.1）：

- `http://127.0.0.1:6060/debug/pprof/`
- 示例 CPU Profile：`curl -s http://127.0.0.1:6060/debug/pprof/profile?seconds=30 > /testbed/output/cpu.pprof`

说明：pprof 服务绑定在容器内的本地环回地址，请使用 `docker exec` 进入容器查看相关端点。

## 输出结构

```
swe_bench_results/
├── predictions.jsonl          # 模型预测结果
├── evaluation_results.json    # 评估结果
├── logs/                      # 执行日志
│   ├── astropy__astropy-13236.log
│   └── ...
└── patches/                   # 生成的补丁文件
    ├── astropy__astropy-13236.patch
    └── ...
```

## 使用方法

### 基本用法

```bash
# 使用预定义的测试集
python run_swe_bench.py --test-set small
python run_swe_bench.py --test-set quick
python run_swe_bench.py --test-set medium

# 指定具体的测试实例
python run_swe_bench.py --test-instances astropy__astropy-13236 django__django-12345

# 自定义配置
python run_swe_bench.py --test-set small --timeout 3600 --max-workers 2 --verbose
```

### 环境变量配置

设置LLM配置（必需）：
```bash
# 推荐：使用统一的API密钥环境变量
export NANO_API_KEY="your-api-key"

# 或者使用特定提供商的API密钥
export ANTHROPIC_API_KEY="your-anthropic-api-key"
# export OPENAI_API_KEY="your-openai-api-key"

# 可选：自定义API端点和模型
export NANO_BASE_URL="https://api.deepseek.com/v1"
export NANO_MODEL="deepseek-chat"
```

### 命令行参数

- `--test-set`: 预定义测试集 (quick/small/medium)
- `--test-instances`: 指定具体测试实例ID
- `--dataset`: SWE-bench数据集名称 (默认: princeton-nlp/SWE-bench_Lite)
- `--timeout`: 每个实例超时时间，秒 (默认: 1800)
- `--max-workers`: 最大并发数 (默认: 1)
- `--run-id`: 运行标识符 (默认: 自动生成)
- `--verbose`: 详细输出
- `--skip-build`: 跳过nano-agent构建
- `--pull-images-only`: 仅拉取Docker镜像

## 架构说明

### SWEBenchRunner 类

主要的测试运行器类，包含以下方法：

- `setup_environment()`: 设置测试环境
- `build_nano_agent()`: 构建nano-agent二进制
- `run_nano_agent_on_issue()`: 在单个问题上运行nano-agent
- `load_swe_bench_issues()`: 加载SWE-bench数据集
- `save_predictions()`: 保存预测结果
- `run_evaluation()`: 运行SWE-bench评估
- `run_full_evaluation()`: 完整的评估流程

### 目录结构

```
swe_bench_test/
├── run_swe_bench.py     # 主测试脚本（简化配置）
├── requirements.txt     # Python依赖
├── README.md           # 说明文档
├── config_templates/    # 配置模板目录
│   └── swebench_config.yaml  # 简化的配置模板
├── SWE-bench/          # SWE-bench仓库（自动克隆）
├── venv/               # Python虚拟环境（自动创建）
└── results/            # 测试结果目录
    ├── predictions.jsonl # 预测结果
    ├── logs/            # 运行日志
    └── reports/         # 评估报告
```

## 故障排除

### 1. Docker镜像拉取失败

```bash
# 手动拉取镜像
docker pull ghcr.nju.edu.cn/epoch-research/swe-bench.eval.x86_64.{instance_id}

# 检查网络连接和镜像仓库访问权限
```

### 2. nano二进制文件不存在

```bash
# 构建nano-agent
cd ../
cargo build --release

# 或指定正确的二进制文件路径
python run_swe_bench.py --nano-binary /path/to/nano --test-instances ...
```

### 3. 内存不足

脚本默认为每个Docker容器分配8GB内存。如果系统内存不足，可以修改 `create_nano_config` 方法中的内存限制。

### 4. Docker权限问题

确保当前用户有Docker权限：
```bash
sudo usermod -aG docker $USER
# 重新登录或重启
```

## 性能优化建议

1. **预拉取镜像**: 使用 `--pull-images-only` 预先拉取所有镜像
2. **跳过构建**: 如果二进制文件已存在，使用 `--skip-build`
3. **并行执行**: 使用 `--parallel` 启用实验性并行执行（谨慎使用）
4. **资源监控**: 监控系统资源使用情况，避免过载

## 示例工作流

```bash
# 1. 设置环境变量
export NANO_API_KEY="your-api-key"

# 2. 预拉取镜像（可选）
python run_swe_bench.py --test-instances astropy__astropy-13236 django__django-12345 --pull-images-only

# 3. 运行快速测试
python run_swe_bench.py --test-set quick --verbose

# 4. 运行指定实例
python run_swe_bench.py --test-instances astropy__astropy-13236 django__django-12345 --timeout 3600

# 5. 跳过构建直接运行（如果二进制已存在）
python run_swe_bench.py --test-set small --skip-build
```

## 贡献

欢迎提交问题和改进建议！

## 参考资料

- [SWE-bench官方仓库](https://github.com/SWE-bench/SWE-bench)
- [Trae-agent实现](https://github.com/bytedance/trae-agent)
- [Docker镜像仓库](https://ghcr.nju.edu.cn/epoch-research/)
