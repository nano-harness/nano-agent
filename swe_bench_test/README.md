# SWE-bench Evaluation for Nano-Agent

[中文](./README.zh-CN.md)

This is an optimized SWE-bench evaluation script that supports using pre-built Docker images and binary mode execution for nano-agent tasks.

## Key Features

- 🐳 **Pre-built Image Support**: Use ready-made Docker images without building from scratch
- ⚡ **Binary Mode**: Support for nano-agent binary mode execution
- 📝 **YAML Configuration**: Flexible configuration file templates
- 🔧 **Multiple Run Modes**: Support for full evaluation, image-only pulling, configuration template generation, etc.
- 📊 **Detailed Statistics**: Provide detailed execution statistics and result reports
- 🏗️ **Automatic Environment Setup**: Automatically create Python virtual environment and install dependencies
- 🔧 **Modular Design**: Class-based design for easy extension and maintenance

## Quick Start

### Prerequisites

- Python 3.8+
- Docker
- Git
- Built nano-agent binary file

### 1. Basic Usage

```bash
# Run full SWE-bench evaluation
python run_swe_bench.py --test-instances astropy__astropy-13236

# Use custom configuration file
python run_swe_bench.py --test-instances astropy__astropy-13236 --config-template config_templates/swebench_config.yaml
```

### 2. Pull Docker Images Only

```bash
# Pre-pull all required Docker images
python run_swe_bench.py --test-instances astropy__astropy-13236 django__django-12345 --pull-images-only
```

### 3. Generate Configuration Template

```bash
# Generate YAML configuration template
python run_swe_bench.py --generate-template my_config.yaml
```

### 4. Skip Build Step

```bash
# Skip build step if nano binary already exists
python run_swe_bench.py --test-instances astropy__astropy-13236 --skip-build
```

The script will automatically:
1. Create Python virtual environment (if not exists)
2. Install necessary dependencies
3. Clone SWE-bench repository (if not exists)
4. Pull pre-built Docker images
5. Build nano-agent (unless using --skip-build)
6. Run evaluation on specified instances
7. Generate result reports

## Command Line Arguments

| Argument | Description | Default |
|----------|-------------|----------|
| `--test-instances` | List of instance IDs to test | Required |
| `--results-dir` | Directory to save results | `./swe_bench_results` |
| `--swe-bench-path` | Path to SWE-bench repository | `./SWE-bench` |
| `--nano-binary` | Path to nano-agent binary | `../target/release/nano` |
| `--config-template` | Path to YAML configuration template | `config_templates/swebench_config.yaml` |
| `--pull-images-only` | Only pull Docker images | False |
| `--generate-template` | Generate configuration template to specified path | None |
| `--skip-build` | Skip nano-agent build step | False |
| `--parallel` | Enable parallel execution (experimental) | False |

## Docker Images

The script uses pre-built Docker images in the format:
```
ghcr.nju.edu.cn/epoch-research/swe-bench.eval.x86_64.{instance_id}
```

For example:
```bash
docker pull ghcr.nju.edu.cn/epoch-research/swe-bench.eval.x86_64.astropy__astropy-13236
```

## Configuration Files

### Configuration Template (swebench_config.yaml)

The project provides a unified configuration template file containing all configuration options required for SWE-bench evaluation:

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
  max_tokens: 200000  # Large context suitable for SWE-bench
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

The configuration file is fully compatible with nano-agent's standard configuration format and supports all nano-agent features.

## Environment Requirements

- Python 3.8+
- Docker
- Required Python packages:
  ```bash
  pip install pyyaml
  ```

## Environment Variables

Set necessary API keys:

```bash
export ANTHROPIC_API_KEY="your-anthropic-api-key"
# or other LLM provider API keys
```

### Profiling (pprof)

When running nano-agent in binary mode inside the SWE-bench container, pprof is enabled automatically via environment variables:

- `NANO_ENABLE_PPROF=true`
- `NANO_PPROF_PORT=6060` (default)

Access from inside the container (local-only binding):

- `http://127.0.0.1:6060/debug/pprof/`
- Example CPU profile: `curl -s http://127.0.0.1:6060/debug/pprof/profile?seconds=30 > /testbed/output/cpu.pprof`

Note: Since the server binds to `127.0.0.1` inside the container, use `docker exec` to inspect pprof endpoints.

## Output Structure

```
swe_bench_results/
├── predictions.jsonl          # Model prediction results
├── evaluation_results.json    # Evaluation results
├── logs/                      # Execution logs
│   ├── astropy__astropy-13236.log
│   └── ...
└── patches/                   # Generated patch files
    ├── astropy__astropy-13236.patch
    └── ...
```

## Usage

### Basic Usage

```bash
# Use predefined test sets
python run_swe_bench.py --test-set small
python run_swe_bench.py --test-set quick
python run_swe_bench.py --test-set medium

# Specify specific test instances
python run_swe_bench.py --test-instances astropy__astropy-13236 django__django-12345

# Custom configuration
python run_swe_bench.py --test-set small --timeout 3600 --max-workers 2 --verbose
```

### Environment Variable Configuration

Set LLM configuration (required):
```bash
# Recommended: Use unified API key environment variable
export NANO_API_KEY="your-api-key"

# Or use provider-specific API keys
export ANTHROPIC_API_KEY="your-anthropic-api-key"
# export OPENAI_API_KEY="your-openai-api-key"

# Optional: Custom API endpoint and model
export NANO_BASE_URL="https://api.deepseek.com/v1"
export NANO_MODEL="deepseek-chat"
```

### Command Line Arguments

- `--test-set`: Predefined test sets (quick/small/medium)
- `--test-instances`: Specify specific test instance IDs
- `--dataset`: SWE-bench dataset name (default: princeton-nlp/SWE-bench_Lite)
- `--timeout`: Timeout per instance in seconds (default: 1800)
- `--max-workers`: Maximum concurrency (default: 1)
- `--run-id`: Run identifier (default: auto-generated)
- `--verbose`: Verbose output
- `--skip-build`: Skip nano-agent build
- `--pull-images-only`: Only pull Docker images

## Architecture Overview

### SWEBenchRunner Class

Main test runner class with the following methods:

- `setup_environment()`: Set up test environment
- `build_nano_agent()`: Build nano-agent binary
- `run_nano_agent_on_issue()`: Run nano-agent on a single issue
- `load_swe_bench_issues()`: Load SWE-bench dataset
- `save_predictions()`: Save prediction results
- `run_evaluation()`: Run SWE-bench evaluation
- `run_full_evaluation()`: Complete evaluation workflow

### Directory Structure

```
swe_bench_test/
├── run_swe_bench.py     # Main test script (simplified configuration)
├── requirements.txt     # Python dependencies
├── README.md           # Documentation (English)
├── README.zh-CN.md     # Documentation (Chinese)
├── config_templates/    # Configuration template directory
│   └── swebench_config.yaml  # Simplified configuration template
├── SWE-bench/          # SWE-bench repository (auto-cloned)
├── venv/               # Python virtual environment (auto-created)
└── results/            # Test results directory
    ├── predictions.jsonl # Prediction results
    ├── logs/            # Execution logs
    └── reports/         # Evaluation reports
```

## Troubleshooting

### 1. Docker Image Pull Failure

```bash
# Manually pull images
docker pull ghcr.nju.edu.cn/epoch-research/swe-bench.eval.x86_64.{instance_id}

# Check network connection and image registry access permissions
```

### 2. Nano Binary Not Found

```bash
# Build nano-agent
cd ../
cargo build --release

# Or specify correct binary path
python run_swe_bench.py --nano-binary /path/to/nano --test-instances ...
```

### 3. Out of Memory

The script allocates 8GB memory per Docker container by default. If system memory is insufficient, you can modify the memory limit in the `create_nano_config` method.

### 4. Docker Permission Issues

Ensure current user has Docker permissions:
```bash
sudo usermod -aG docker $USER
# Re-login or restart
```

## Performance Optimization Tips

1. **Pre-pull Images**: Use `--pull-images-only` to pre-pull all images
2. **Skip Build**: Use `--skip-build` if binary already exists
3. **Parallel Execution**: Use `--parallel` for experimental parallel execution (use with caution)
4. **Resource Monitoring**: Monitor system resource usage to avoid overload

## Example Workflow

```bash
# 1. Set environment variables
export NANO_API_KEY="your-api-key"

# 2. Pre-pull images (optional)
python run_swe_bench.py --test-instances astropy__astropy-13236 django__django-12345 --pull-images-only

# 3. Run quick test
python run_swe_bench.py --test-set quick --verbose

# 4. Run specific instances
python run_swe_bench.py --test-instances astropy__astropy-13236 django__django-12345 --timeout 3600

# 5. Skip build and run directly (if binary exists)
python run_swe_bench.py --test-set small --skip-build
```

## Contributing

Welcome to submit issues and improvement suggestions!

## References

- [SWE-bench Official Repository](https://github.com/SWE-bench/SWE-bench)
- [Trae-agent Implementation](https://github.com/bytedance/trae-agent)
- [Docker Image Registry](https://ghcr.nju.edu.cn/epoch-research/)
