#!/usr/bin/env python3
"""
SWE-bench evaluation runner for nano-agent.

This script runs nano-agent on SWE-bench instances using Docker containers
with proper environment variable configuration instead of complex config files.
"""

import os
import json
import subprocess
import tempfile
import shutil
import sys
import time
from pathlib import Path
from typing import Dict, List, Optional

from datasets import load_dataset

# 默认配置
DEFAULT_DATASET = "princeton-nlp/SWE-bench_Verified"
DEFAULT_DATASET_SPLIT = "test"
DEFAULT_TIMEOUT = 1800  # 30分钟
DEFAULT_MAX_WORKERS = 1
DEFAULT_MODEL_NAME = "kimi-k2-0711-preview"
DOCKER_REGISTRY = "slimshetty/swebench-verified"
DOCKER_IMAGE_PREFIX = "sweb.eval.x86_64"

# 预定义测试实例集合
TEST_INSTANCE_SETS = {
    "quick": ["astropy__astropy-14365"],
    "small": ["astropy__astropy-14365", "django__django-10914", "sympy__sympy-20590"],
    "medium": [
        "astropy__astropy-14365", "django__django-10914", "sympy__sympy-20590",
        "matplotlib__matplotlib-25311", "scikit-learn__scikit-learn-10297"
    ],
    "large": [
        # Resolved instances
        "django__django-10880",
        "django__django-10914",
        "django__django-11133",
        "matplotlib__matplotlib-13989",
        "matplotlib__matplotlib-14623",
        "matplotlib__matplotlib-23314",
        "matplotlib__matplotlib-24149",
        "matplotlib__matplotlib-25311",
        "pydata__xarray-2905",
        "pydata__xarray-3095",
        "pytest-dev__pytest-5262",
        "pytest-dev__pytest-5631",
        "scikit-learn__scikit-learn-10297",
        "scikit-learn__scikit-learn-10844",
        "scikit-learn__scikit-learn-10908",
        "sympy__sympy-11618",
        "sympy__sympy-12096",
        "sympy__sympy-12419",
        "sympy__sympy-20590",
        # Unresolved instances
        "astropy__astropy-12907",
        "astropy__astropy-13033",
        "astropy__astropy-13236",
        "astropy__astropy-14365",
        "astropy__astropy-14995",
        "django__django-10097",
        "django__django-10554",
        "django__django-11179",
        "matplotlib__matplotlib-20488",
        "psf__requests-2317",
        "sphinx-doc__sphinx-10323",
        "sphinx-doc__sphinx-10435"
    ]
}

def get_docker_image_name(instance_id: str) -> str:
    """生成Docker镜像名称，使用 tag 形式 slimshetty/swebench-verified:sweb.eval.x86_64.<instance_id>"""
    return f"{DOCKER_REGISTRY}:{DOCKER_IMAGE_PREFIX}.{instance_id}"


class SWEBenchRunner:
    """SWE-bench test runner for nano-agent."""

    def __init__(self, project_root: str, test_dir: str,
                 test_instances: Optional[List[str]] = None,
                 dataset: str = DEFAULT_DATASET,
                 timeout: int = DEFAULT_TIMEOUT,
                 max_workers: int = DEFAULT_MAX_WORKERS,
                 run_id: str = None,
                 verbose: bool = True,
                 swe_bench_path: Optional[str] = None,
                 nano_binary: Optional[str] = None):
        self.project_root = Path(project_root).resolve()
        self.test_dir = Path(test_dir).resolve()
        self.results_dir = self.test_dir / "results"
        # Determine SWE-bench repo path: CLI arg > env SWE_BENCH_PATH > default under test_dir
        swe_repo_override = swe_bench_path or os.getenv("SWE_BENCH_PATH")
        if swe_repo_override:
            self.swe_bench_repo = Path(swe_repo_override).resolve()
        else:
            self.swe_bench_repo = self.test_dir / "SWE-bench"
        self.venv_python = self.test_dir / "venv" / "bin" / "python3"

        self.nano_binary = None
        if nano_binary:
            self.nano_binary = Path(nano_binary).resolve()  # 确保使用绝对路径
            # 验证nano二进制文件
            if not self.nano_binary.exists():
                print(f"Warning: nano binary not found at {self.nano_binary}")

        # 测试配置
        self.test_instances = test_instances or TEST_INSTANCE_SETS["small"]
        self.dataset = dataset
        self.timeout = timeout
        self.max_workers = max_workers
        self.run_id = run_id or f"nano-agent-{int(time.time())}"
        self.verbose = verbose
        # Model name used both for env and for predictions metadata
        self.model_name = os.getenv("NANO_MODEL", DEFAULT_MODEL_NAME)

        # Create necessary directories
        self.results_dir.mkdir(exist_ok=True)

    def setup_environment(self) -> bool:
        """Setup the testing environment."""
        print("Setting up SWE-bench testing environment...")

        # Check if venv exists
        if not self.venv_python.exists():
            print("Creating Python virtual environment...")
            subprocess.run([
                sys.executable, "-m", "venv",
                str(self.test_dir / "venv")
            ], check=True)

            # Install required packages
            print("Installing required packages...")
            subprocess.run([
                str(self.venv_python), "-m", "pip", "install",
                "datasets", "docker"
            ], check=True)

        # Check if SWE-bench repo exists
        if not self.swe_bench_repo.exists():
            print("Cloning SWE-bench repository...")
            subprocess.run([
                "git", "clone",
                "https://github.com/princeton-nlp/SWE-bench.git",
                str(self.swe_bench_repo)
            ], check=True)

            # Install SWE-bench
            subprocess.run([
                str(self.venv_python), "-m", "pip", "install", "-e", "."
            ], cwd=self.swe_bench_repo, check=True)

        return True

    def pull_docker_image(self, instance_id: str) -> bool:
        """Pull pre-built Docker image for the instance."""
        image_name = get_docker_image_name(instance_id)
        print(f"Pulling Docker image: {image_name}")

        try:
            result = subprocess.run(
                ["docker", "pull", image_name],
                capture_output=True,
                text=True,
                timeout=300  # 5 minutes timeout
            )

            if result.returncode != 0:
                print(f"Failed to pull image {image_name}: {result.stderr}")
                return False

            print(f"Successfully pulled: {image_name}")
            return True

        except subprocess.TimeoutExpired:
            print(f"Timeout pulling image {image_name}")
            return False
        except Exception as e:
            print(f"Exception pulling image {image_name}: {e}")
            return False

    def create_swe_bench_prompt(self, issue: Dict) -> str:
        """创建SWE-bench的详细提示"""
        instance_id = issue['instance_id']
        problem_statement = issue['problem_statement']
        repo = issue['repo']

        prompt = f"""# SWE-bench Issue: {instance_id}

## Repository: {repo}

## Problem Statement:
{problem_statement}

## Critical Rules (务必遵守的硬性规则):
- 严禁修改、删除或跳过任何测试：不得改动 tests/、testing/、*/tests/*、test_*.py、conftest.py、pytest.ini 等与测试相关的任何文件与配置；不得添加或保留 @pytest.mark.skip、xfail、过滤器或以其他方式规避测试；不得修改断言内容或测试逻辑。
- 只允许修改产品/库实现代码：将改动限制在源码目录（如项目主包目录、src/、<package>/**）；不要改动 docs/、examples/、.github/、scripts/、benchmarks/、ci/ 等非产品代码目录。
- 最小化变更面：定位根因后以最小必要改动修复问题，避免大范围重构、代码风格/格式化改动、无关重命名；除非问题根因即为 API 契约错误，否则保持 API/行为向后兼容。
- 若看似测试存在问题：请在最终说明中阐述理由，但仍通过修改实现代码去满足既有测试/行为约定；绝不修改测试文件或测试配置。

## Planning Requirement (规划要求):
在开始任何修改前，你必须先输出一个明确的计划，包含：
1. 关键假设与理解：对问题的核心理解和预期行为
2. 探索清单：需要查看的文件/目录列表，按优先级排序
3. 验证步骤：如何确认修复有效性的具体方法
4. 风险评估：潜在副作用和回滚方案
5. 成功标准：明确的完成判据

## Task Instructions:
你需要在不修改任何测试的前提下修复仓库中的软件缺陷。请严格按照以下工作流循环执行：

### Phase 1: Planning & Analysis
1. **输出计划**：按照上述规划要求，先给出完整的解决计划
2. **分析问题**：细读问题描述并明确预期行为与实际行为的差异
3. **探索代码库**：使用文件系统与搜索工具定位相关模块与关键路径，理解当前实现及其依赖关系

### Phase 2: Root Cause & Solution
4. **确认根因**：基于调用路径与条件分支，找出导致错误的具体代码位置与机制
5. **提出方案**：给出最小变更方案，说明预期影响和为什么此方案能解决问题
6. **实施修复**：在实现代码中进行最小必要的改动以满足既有行为契约与测试预期

### Phase 3: Verification & Iteration
7. **自检验证**：检查边界条件与潜在副作用，确保不破坏现有功能与兼容性
8. **更新计划**：如果发现问题或需要调整，更新计划与假设，明确下一步动作
9. **迭代循环**：重复步骤4-8直到满足成功标准

## Acceptance Criteria (验收标准):
- 未对任何测试文件、测试配置或 CI 相关脚本进行修改或规避。
- 所有改动集中在实现代码，包含必要且简洁的注释，解释设计取舍。
- 修复后应满足问题描述中的场景，并保持对现有行为的向后兼容。

## Final Output Requirements (最终输出要求):
在完成修复后，你必须提供：
1. **根因分析**：问题的具体原因和触发条件
2. **变更摘要**：修改的文件和关键代码行，以及修改理由
3. **有效性说明**：为什么此修复能解决问题并满足既有测试
4. **风险评估**：潜在副作用、兼容性影响和后续建议
5. **自检清单确认**：明确声明"未改动任何测试/CI/配置文件"

## Working Environment:
- 当前工作目录: /testbed（仓库代码位于此处）
- 你可使用可用的工具进行浏览、理解与修改代码
- 这是一个预加载目标仓库的 SWE-bench 评测环境

请严格遵守上述硬性规则，聚焦修复实现代码中的真实缺陷，且不要对任何测试进行修改或规避。"""
        return prompt

    def setup_environment_variables(self) -> Dict[str, str]:
        """设置nano-agent的环境变量配置"""
        # 从当前环境获取API配置
        api_key = (os.getenv("NANO_API_KEY") or
                  os.getenv("ANTHROPIC_API_KEY") or
                  os.getenv("OPENAI_API_KEY"))

        if not api_key:
            raise ValueError("No API key found. Set NANO_API_KEY, ANTHROPIC_API_KEY, or OPENAI_API_KEY")

        # 构建环境变量字典
        env_vars = os.environ.copy()
        env_vars.update({
            # Core LLM settings - 根据 config.go 中的环境变量名称
            "NANO_API_KEY": api_key,
            "NANO_BASE_URL": os.getenv("NANO_BASE_URL", "https://api.deepseek.com/v1"),
            "NANO_MODEL": self.model_name,
            # 关闭调试日志以避免打印System Prompt，保留Info级别日志用于实时查看
            "NANO_VERBOSE": "false",

            # Performance settings - 根据 config.go 中的环境变量名称
            "NANO_MAX_FILE_SIZE": "10485760",  # 10MB
            "NANO_RESPONSE_TIMEOUT": "120",   # 2 minutes
            "NANO_HTTP_TIMEOUT": "60",        # 1 minute
            "NANO_MAX_TOOL_CALL_DEPTH": "20",
            "NANO_MAX_TURN_DURATION": "30",   # 30 minutes

            # Context settings - 根据 config.go 中的环境变量名称
            "NANO_CONTEXT_MAX_TOKENS": "200000",
            "NANO_CONTEXT_ENABLE_COMPRESSION": "true",
            "NANO_CONTEXT_PRESERVE_RECENT_TURNS": "4",

            # Safety settings - 根据 config.go 中的环境变量名称
            "NANO_CONFIRM_DESTRUCTIVE": "false",

            # Tool settings - 根据 config.go 中的环境变量名称
            "NANO_ENABLED_TOOLS": "filesystem,search,system",

            # Disable features not needed for SWE-bench - 根据 config.go 中的环境变量名称
            "NANO_ENABLE_MCP": "false",

            # Additional config.go environment variables
            "NANO_READ_FILE_MAX_LINES": "1000",
            "NANO_SEARCH_MAX_RESULTS": "50",
            "NANO_WEB_REQUEST_TIMEOUT": "30",
            "NANO_WEB_SEARCH_TIMEOUT": "30",
            "NANO_WEB_MAX_CONTENT_SIZE": "1048576",  # 1MB
            "NANO_WEB_SEARCH_MAX_RESULTS": "10",
            "NANO_FILE_DIFF_MAX_LINES": "500",
            "NANO_GIT_MAX_LOG_ENTRIES": "100",

            # Profiling (pprof) 设置：启用并在二进制模式下监听容器本地端口
            "NANO_ENABLE_PPROF": os.getenv("NANO_ENABLE_PPROF", "true"),
            "NANO_PPROF_PORT": os.getenv("NANO_PPROF_PORT", "6060"),
        })

        return env_vars

    def build_nano_in_docker(self, instance_id: str) -> str:
        """在Docker容器内构建nano agent二进制文件以确保兼容性"""
        print(f"Building nano agent inside Docker container for {instance_id}...")

        # 创建临时构建目录
        build_dir = tempfile.mkdtemp(prefix=f"nano_build_{instance_id}_")

        try:
            # 复制整个nano-agent源代码到构建目录
            source_dir = os.path.join(build_dir, "nano-agent")
            shutil.copytree(
                self.project_root,
                source_dir,
                ignore=shutil.ignore_patterns('.git', 'bin', 'swe_bench_test', '__pycache__', '*.pyc')
            )

            # 设置Docker构建命令 - 使用Golang官方构建镜像进行构建（SWE-bench镜像内没有Go）
            builder_image = os.getenv("NANO_GO_BUILDER_IMAGE", "golang:1.24-bullseye")
            platform = os.getenv("NANO_DOCKER_PLATFORM", "linux/amd64")  # 确保与评测镜像架构一致

            go_proxy = os.getenv("NANO_GO_PROXY", "https://proxy.golang.org,direct")
            build_script = (
                "set -euo pipefail; "
                "export GOTOOLCHAIN=auto; "
                f"go env -w GOPROXY={go_proxy} >/dev/null 2>&1 || true; "
                "go mod download; "
                "CGO_ENABLED=0 GOOS=linux GOARCH=amd64 "
                "go build -ldflags='-s -w' -o /workspace/output/nano ./cmd/nano"
            )

            build_cmd = [
                "docker", "run", "--rm", "--platform", platform,
                "-v", f"{source_dir}:/workspace/nano-agent:ro",
                "-v", f"{build_dir}:/workspace/output",
                "-w", "/workspace/nano-agent",
                builder_image,
                "bash", "-c", build_script,
            ]

            result = subprocess.run(
                build_cmd,
                capture_output=True,
                text=True,
                timeout=300  # 5分钟构建超时
            )

            if result.returncode != 0:
                print(f"❌ Failed to build nano in Docker (using {builder_image} on {platform}): {result.stderr}")
                return ""

            # 获取构建的二进制文件路径
            nano_binary_path = os.path.join(build_dir, "nano")
            if os.path.exists(nano_binary_path):
                print(f"✅ Successfully built nano binary in Docker")
                return nano_binary_path
            else:
                print(f"❌ Built binary not found at expected location")
                return ""

        except Exception as e:
            print(f"❌ Exception during Docker build: {e}")
            return ""
        finally:
            # 清理构建目录（除了二进制文件）
            if 'nano_binary_path' in locals() and os.path.exists(nano_binary_path):
                # 保留二进制文件，删除其他文件
                source_dir_to_remove = os.path.join(build_dir, "nano-agent")
                if os.path.exists(source_dir_to_remove):
                    shutil.rmtree(source_dir_to_remove, ignore_errors=True)

    def run_standalone_evaluation(self, predictions_path: str, instance_ids: Optional[List[str]] = None):
        """Runs the SWE-bench evaluation script on a given predictions file."""
        print(f"\n--- Running Evaluation-Only Mode ---")
        print(f"Predictions file: {predictions_path}")

        eval_script_path = self.swe_bench_repo / "swebench" / "harness" / "run_evaluation.py"
        if not eval_script_path.exists():
            print(f"❌ Evaluation script not found at: {eval_script_path}")
            sys.exit(1)

        # Ensure predictions path is absolute
        predictions_path = str(Path(predictions_path).resolve())

        cmd = [
            str(self.venv_python),
            str(eval_script_path),
            "--predictions_path", predictions_path,
            "--dataset_name", self.dataset,
            "--split", DEFAULT_DATASET_SPLIT,
            "--run_id", self.run_id,
            "--max_workers", str(self.max_workers),
            "--timeout", str(self.timeout),
            "--namespace", DOCKER_REGISTRY,
            "--offline", "true",  # 默认使用离线模式
        ]

        if instance_ids:
            cmd.extend(["--instance_ids"] + instance_ids)

        print(f"\nExecuting evaluation command:\n{' '.join(cmd)}")

        try:
            # Use the SWE-bench repo as the working directory
            subprocess.run(cmd, check=True, cwd=self.swe_bench_repo)
            print("\n✅ Evaluation completed successfully.")
        except subprocess.CalledProcessError as e:
            print(f"\n❌ Evaluation failed with exit code {e.returncode}")
        except Exception as e:
            print(f"\n❌ An unexpected error occurred during evaluation: {e}")

    def run_nano_agent_on_issue(self, issue: Dict) -> str:
        """Run nano agent on a single SWE-bench issue."""
        instance_id = issue['instance_id']
        repo = issue['repo']

        print(f"\n=== Running nano agent for: {instance_id} ===")

        # 首先拉取Docker镜像
        if not self.pull_docker_image(instance_id):
            print(f"❌ Failed to pull Docker image for {instance_id}")
            return ""

        # 在Docker容器内构建兼容的二进制文件（或复用缓存/用户提供的二进制）
        if getattr(self, 'skip_build', False):
            # 跳过构建：使用用户提供的本地二进制
            docker_nano_binary = str(self.nano_binary)
            if not os.path.exists(docker_nano_binary):
                print(f"❌ Provided nano binary not found: {docker_nano_binary}")
                return ""
        else:
            # 优先使用缓存的一次性构建结果
            docker_nano_binary = getattr(self, '_cached_docker_binary', None)
            if not docker_nano_binary or not os.path.exists(docker_nano_binary):
                docker_nano_binary = self.build_nano_in_docker(instance_id)
                if not docker_nano_binary:
                    print(f"❌ Failed to build nano binary for {instance_id}")
                    return ""
                # 缓存以复用到后续实例
                self._cached_docker_binary = docker_nano_binary

        # 创建临时工作目录
        temp_dir = tempfile.mkdtemp(prefix=f"nano_agent_{instance_id}_")
        try:
            # 生成SWE-bench提示
            prompt = self.create_swe_bench_prompt(issue)

            # 设置Docker命令和环境变量
            image_name = get_docker_image_name(instance_id)

            # 设置环境变量
            env_vars = self.setup_environment_variables()
            # 从数据集中仅透传 base_commit 到 nano 的环境变量，供 CLI 使用
            base_commit = issue.get('base_commit')
            if base_commit:
                env_vars['NANO_BASE_COMMIT'] = base_commit

            # 创建输出目录
            output_dir = os.path.join(temp_dir, "output")
            os.makedirs(output_dir, exist_ok=True)

            # 构建Docker命令 - 使用二进制模式并传递提示作为参数
            # 注意：SWE-bench容器内代码仓库位于/testbed，不是/workspace
            testbed_dir = "/testbed"  # SWE-bench标准的代码仓库目录

            # 构建nano agent命令行参数
            nano_args = [
                "/usr/local/bin/nano",
                "--binary-mode",
                "--output-dir", "output",  # 输出相对于testbed目录
                prompt  # 添加提示作为最后一个参数
            ]

            docker_cmd = [
                "docker", "run", "--rm",
                "--memory=8g",      # 限制内存使用
                "--cpus=2",         # 限制CPU使用
                "-v", f"{docker_nano_binary}:/usr/local/bin/nano:ro",  # 挂载兼容的二进制文件
                "-v", f"{output_dir}:{testbed_dir}/output",           # 在代码仓库内创建输出目录
                "-w", testbed_dir,  # 工作目录设置为代码仓库所在路径
                "--user", "root",   # SWE-bench容器通常使用root用户
                image_name,
            ] + nano_args

            # 添加环境变量到Docker命令
            env_args = []
            for key, value in env_vars.items():
                if key.startswith("NANO_") or key.endswith("_API_KEY"):
                    env_args.extend(["-e", f"{key}={value}"])

            # 在docker run之后、image_name之前插入环境变量参数
            final_cmd = docker_cmd[:3] + env_args + docker_cmd[3:]

            print(f"Executing nano agent with binary mode...")
            # 打印脱敏后的命令，避免泄露API Key
            def redact(cmd_parts: List[str]) -> List[str]:
                redacted = []
                for part in cmd_parts:
                    if part.startswith("NANO_API_KEY="):
                        redacted.append("NANO_API_KEY=****")
                    elif part.startswith("OPENAI_API_KEY="):
                        redacted.append("OPENAI_API_KEY=****")
                    elif part.startswith("ANTHROPIC_API_KEY="):
                        redacted.append("ANTHROPIC_API_KEY=****")
                    else:
                        redacted.append(part)
                return redacted
            preview_cmd = redact(final_cmd)
            print(f"Command: {' '.join(preview_cmd[:10])}... (truncated)")
            # 不再打印完整命令以免泄露敏感信息
            print("=" * 80)
            start_time = time.time()

            # 使用实时流式输出，显示nano agent的执行日志
            process = subprocess.Popen(
                final_cmd,
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,  # 合并stderr到stdout
                text=True,
                bufsize=1,  # 行缓冲
                universal_newlines=True
            )

            # 实时打印输出并收集用于返回
            output_lines = []
            try:
                while True:
                    line = process.stdout.readline()
                    if not line:
                        break
                    # 过滤System Prompt相关的控制台输出（但不影响轨迹日志中的system prompt记录）
                    lower = line.lower()
                    if "system prompt" in lower and ("using system prompt" in lower or "system prompt content" in lower):
                        continue
                    # 实时打印输出，添加前缀便于识别
                    print(f"[nano-agent] {line.rstrip()}")
                    output_lines.append(line)

                # 等待进程结束
                return_code = process.wait(timeout=self.timeout + 60)

            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
                print(f"❌ Timeout running nano agent for {instance_id}")
                return ""

            output = ''.join(output_lines)
            elapsed = time.time() - start_time
            print("=" * 80)
            print(f"Execution completed in {elapsed:.1f}s with return code: {return_code}")

            if return_code != 0:
                print(f"❌ Error running nano agent for {instance_id}:")
                print(f"Return code: {return_code}")
                print(f"Output (last 1000 chars): {output[-1000:]}")
                return ""

            # 检查生成的patch文件
            patch_file = os.path.join(output_dir, "solution.patch")
            if os.path.exists(patch_file):
                with open(patch_file, "r") as f:
                    patch_content = f.read()
                print(f"✅ Successfully generated patch for {instance_id}")
                return patch_content
            else:
                print(f"⚠️  No patch file generated for {instance_id}")
                return output  # 返回标准输出作为fallback

        except subprocess.TimeoutExpired:
            print(f"❌ Timeout running nano agent for {instance_id}")
            return ""
        except Exception as e:
            print(f"❌ Exception running nano agent for {instance_id}: {e}")
            return ""
        finally:
            # 在清理临时目录前，尽量保存trajectory.json到持久化结果目录
            try:
                traj_src = os.path.join(temp_dir, "output", "trajectory.json")
                if os.path.exists(traj_src):
                    traj_dir = os.path.join(str(self.results_dir), "trajectories", instance_id)
                    os.makedirs(traj_dir, exist_ok=True)
                    traj_dst = os.path.join(traj_dir, "trajectory.json")

                    # 优化trajectory文件，压缩流式输出内容
                    self.optimize_trajectory_file(traj_src, traj_dst)

                    if self.verbose:
                        print(f"🧭 Saved optimized trajectory to: {traj_dst}")
                else:
                    if self.verbose:
                        print("ℹ️  No trajectory.json found to persist")
            except Exception as e:
                print(f"⚠️  Failed to persist trajectory.json: {e}")

            # 清理临时目录（保留/复用nano二进制，避免重复构建）
            shutil.rmtree(temp_dir, ignore_errors=True)

    def optimize_trajectory_file(self, src_path: str, dst_path: str) -> None:
        """优化trajectory文件，压缩流式输出内容"""
        try:
            with open(src_path, 'r', encoding='utf-8') as f:
                trajectory = json.load(f)

            if not isinstance(trajectory, list):
                # 如果不是列表格式，直接复制
                shutil.copy2(src_path, dst_path)
                return

            optimized_trajectory = []
            stream_content_buffer = []
            last_stream_content = ""
            stream_content_count = 0
            max_stream_events = 50  # 限制流式事件数量
            max_content_length = 1000  # 限制单个内容长度

            for event in trajectory:
                if not isinstance(event, dict):
                    optimized_trajectory.append(event)
                    continue

                event_type = event.get('type', '')
                content = event.get('content', '')

                # 处理流式内容事件
                if event_type == 'stream_content':
                    stream_content_count += 1

                    # 跳过重复的流式内容
                    if content == last_stream_content:
                        continue

                    # 限制流式事件数量
                    if stream_content_count > max_stream_events:
                        stream_content_buffer.append(content)
                        continue

                    # 限制内容长度
                    if len(content) > max_content_length:
                        # 截断长内容
                        truncated_content = content[:max_content_length//2] + "\n... [content truncated] ...\n" + content[-max_content_length//2:]
                        event = event.copy()
                        event['content'] = truncated_content
                        event['meta'] = event.get('meta', {})
                        event['meta']['truncated'] = True
                        event['meta']['original_length'] = len(content)

                    last_stream_content = content
                    optimized_trajectory.append(event)

                # 处理token_stats事件 - 只保留重要的统计信息
                elif event_type == 'token_stats':
                    # 跳过大部分token统计事件，只保留少数几个
                    if len([e for e in optimized_trajectory if e.get('type') == 'token_stats']) < 5:
                        optimized_trajectory.append(event)

                # 保留其他重要事件
                else:
                    optimized_trajectory.append(event)

            # 如果有累积的流式内容，添加摘要
            if stream_content_buffer:
                combined_content = ''.join(stream_content_buffer)
                if len(combined_content) > max_content_length:
                    # 压缩长内容
                    start = combined_content[:max_content_length//2]
                    end = combined_content[-max_content_length//2:]
                    summary_content = f"{start}\n... [{len(combined_content) - max_content_length} characters omitted] ...\n{end}"
                else:
                    summary_content = combined_content

                summary_event = {
                    'type': 'stream_content_summary',
                    'content': summary_content,
                    'meta': {
                        'role': 'assistant',
                        'compressed': True,
                        'original_events': len(stream_content_buffer),
                        'original_length': len(combined_content)
                    },
                    'timestamp': int(time.time() * 1000000000)  # nanoseconds
                }
                optimized_trajectory.append(summary_event)

            # 保存优化后的trajectory
            with open(dst_path, 'w', encoding='utf-8') as f:
                json.dump(optimized_trajectory, f, indent=2, ensure_ascii=False)

            # 计算压缩比例
            original_size = os.path.getsize(src_path)
            optimized_size = os.path.getsize(dst_path)
            compression_ratio = (original_size - optimized_size) / original_size * 100

            if self.verbose:
                print(f"📊 Trajectory optimization: {original_size} → {optimized_size} bytes ({compression_ratio:.1f}% reduction)")

        except Exception as e:
            if self.verbose:
                print(f"⚠️  Trajectory optimization failed, using original: {e}")
            # 如果优化失败，直接复制原文件
            shutil.copy2(src_path, dst_path)

    def pull_images_only(self) -> bool:
        """仅拉取Docker镜像"""
        print(f"🐳 Pulling Docker images for {len(self.test_instances)} instances...")
        print(f"Registry: {DOCKER_REGISTRY}")

        failed_pulls = []
        successful_pulls = []

        for i, instance_id in enumerate(self.test_instances, 1):
            print(f"\n[{i}/{len(self.test_instances)}] Pulling: {instance_id}")
            if self.pull_docker_image(instance_id):
                successful_pulls.append(instance_id)
                print(f"✅ Successfully pulled: {instance_id}")
            else:
                failed_pulls.append(instance_id)
                print(f"❌ Failed to pull: {instance_id}")

        print(f"\n📊 Pull Summary:")
        print(f"✅ Successful: {len(successful_pulls)}/{len(self.test_instances)}")
        if failed_pulls:
            print(f"❌ Failed: {len(failed_pulls)}")
            print(f"Failed instances: {', '.join(failed_pulls)}")

        return len(failed_pulls) == 0

    def load_swe_bench_issues(self) -> List[Dict]:
        """Load SWE-bench issues from the dataset."""
        print(f"Loading {self.dataset} dataset...")
        try:
            dataset = load_dataset(self.dataset, split=DEFAULT_DATASET_SPLIT)
            issues = [issue for issue in dataset if issue['instance_id'] in self.test_instances]
            print(f"Loaded {len(issues)} test instances")
            return issues
        except Exception as e:
            print(f"Failed to load dataset: {e}")
            return []

    def save_predictions(self, predictions: List[Dict]) -> Path:
        """Save predictions to JSONL file."""
        predictions_file = self.results_dir / 'predictions.jsonl'
        with open(predictions_file, 'w') as f:
            for pred in predictions:
                f.write(json.dumps(pred) + '\n')
        print(f"Saved {len(predictions)} predictions to {predictions_file}")
        return predictions_file

    def run_evaluation(self, predictions_file: Path) -> bool:
        """Run SWE-bench evaluation on predictions."""
        print("\n=== Running SWE-bench evaluation ===")

        eval_cmd = [
            str(self.venv_python),
            '-m', 'swebench.harness.run_evaluation',
            '--dataset_name', self.dataset,
            '--instance_ids'
        ] + self.test_instances + [
            '--predictions_path', str(predictions_file.absolute()),
            '--run_id', self.run_id,
            '--max_workers', str(self.max_workers),
            '--namespace', DOCKER_REGISTRY,
            '--report_dir', str(self.results_dir.absolute()),
            '--offline', 'true'  # 默认使用离线模式
        ]

        try:
            subprocess.run(
                eval_cmd,
                cwd=self.swe_bench_repo,
                check=True,
                capture_output=False,  # Show output in real-time
                text=True
            )
            print("✅ Evaluation completed successfully")
            return True
        except subprocess.CalledProcessError as e:
            print(f"❌ Evaluation failed: {e}")
            if e.stdout:
                print(f"Stdout: {e.stdout}")
            if e.stderr:
                print(f"Stderr: {e.stderr}")
            return False

    def run_full_evaluation(self) -> bool:
        """Run the complete SWE-bench evaluation pipeline."""
        print(f"📋 Test instances: {', '.join(self.test_instances)}")
        print(f"🐳 Using Docker registry: {DOCKER_REGISTRY}")

        # Setup environment
        if not self.setup_environment():
            return False

        # Build nano agent once in Docker (if not skipping)
        docker_binary_path = None
        if not getattr(self, 'skip_build', False):
            print("🔨 Building nano agent in Docker (one-time build)...")
            # Use first instance for build, binary will be reused
            docker_binary_path = self.build_nano_in_docker(self.test_instances[0])
            if not docker_binary_path:
                print("❌ Failed to build nano binary")
                return False
            print(f"✅ Built nano binary: {docker_binary_path}")
        else:
            print("⏭️  Skipping nano agent build")
            # Verify nano binary exists when skipping build
            if not self.nano_binary.exists():
                print(f"❌ Nano binary not found: {self.nano_binary}")
                print("Please build the binary first or remove --skip-build flag")
                return False

        # Cache the built binary path for reuse
        self._cached_docker_binary = docker_binary_path

        # Load test issues
        issues = self.load_swe_bench_issues()
        if not issues:
            print("❌ No test issues loaded")
            return False

        # Run nano agent on each issue
        print("\n🤖 Running nano agent on SWE-bench issues...")
        predictions = []
        successful_runs = 0

        for i, issue in enumerate(issues, 1):
            instance_id = issue['instance_id']
            print(f"\n--- Processing {i}/{len(issues)}: {instance_id} ---")

            patch = self.run_nano_agent_on_issue(issue)

            prediction = {
                "model_name_or_path": self.model_name,
                "instance_id": instance_id,
                "model_patch": patch
            }
            predictions.append(prediction)

            if patch.strip():  # Non-empty patch
                successful_runs += 1

        print(f"\n📊 Summary: {successful_runs}/{len(issues)} instances generated patches")

        if not predictions:
            print("❌ No predictions generated")
            return False

        # Save predictions
        predictions_file = self.save_predictions(predictions)

        # Run evaluation
        success = self.run_evaluation(predictions_file)

        if success:
            print(f"\n🎉 Evaluation completed! Results saved in: {self.results_dir}")
            print(f"📈 Generated patches for {successful_runs}/{len(issues)} instances")
        else:
            print(f"\n❌ Evaluation failed. Check logs in: {self.results_dir}")

        return success

    def validate_environment(self) -> bool:
        """验证本地 SWE-bench 评测环境是否准备就绪。"""
        print("\n🔍 Validating local SWE-bench evaluation environment...")
        ok = True
        # 1) Docker 可用
        try:
            out = subprocess.run(["docker", "--version"], capture_output=True, text=True)
            if out.returncode != 0:
                print("❌ Docker not available")
                ok = False
            else:
                print(f"✅ Docker available: {out.stdout.strip()}")
        except Exception as e:
            print(f"❌ Docker check failed: {e}")
            ok = False
        # 2) 虚拟环境
        if self.venv_python.exists():
            print(f"✅ Python venv found: {self.venv_python}")
        else:
            print(f"⚠️  Python venv not found at: {self.venv_python}")
        # 3) SWE-bench 仓库
        if self.swe_bench_repo.exists():
            print(f"✅ SWE-bench repo found: {self.swe_bench_repo}")
            harness = self.swe_bench_repo / "swebench" / "harness" / "run_evaluation.py"
            if harness.exists():
                print("✅ SWE-bench harness present")
            else:
                print("❌ SWE-bench harness not found under repo")
                ok = False
        else:
            print(f"❌ SWE-bench repo not found at: {self.swe_bench_repo}")
            ok = False
        # 4) venv 中的 swebench 与 datasets 可导入
        if self.venv_python.exists() and self.swe_bench_repo.exists():
            try:
                test_code = "import datasets, swebench; print('ok')"
                out = subprocess.run([str(self.venv_python), "-c", test_code], cwd=self.swe_bench_repo, capture_output=True, text=True)
                if out.returncode == 0 and 'ok' in out.stdout:
                    print("✅ venv can import datasets and swebench")
                else:
                    print("⚠️  venv cannot import swebench or datasets; will install during setup_environment")
            except Exception as e:
                print(f"⚠️  Import test failed: {e}")
        # 5) 测试 datasets 加载访问（不强制下载）
        try:
            _ = load_dataset(self.dataset, split=DEFAULT_DATASET_SPLIT)
            print(f"✅ Dataset accessible: {self.dataset} ({DEFAULT_DATASET_SPLIT})")
        except Exception as e:
            print(f"⚠️  Dataset access failed (will attempt during run): {e}")
        return ok


def main():
    """Main entry point."""
    import argparse

    parser = argparse.ArgumentParser(
        description='Run nano-agent SWE-bench evaluation (simplified configuration)',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Environment Variables (LLM Configuration):
  NANO_API_KEY                         - API key for LLM (preferred)
  ANTHROPIC_API_KEY or OPENAI_API_KEY  - Alternative API keys
  NANO_BASE_URL                        - API base URL (optional)
  NANO_MODEL                           - Model name (optional)
  SWE_BENCH_PATH                       - Local SWE-bench repo path (optional)

Examples:
  # Run small test set
  python run_swe_bench.py --test-set small

  # Run specific instances
  python run_swe_bench.py --test-instances astropy__astropy-13236 django__django-10914

  # Run evaluation only on a predictions file
  python run_swe_bench.py --evaluation-only ./results/predictions.jsonl

  # Run evaluation for specific instances from a predictions file
  python run_swe_bench.py --evaluation-only ./results/predictions.jsonl --test-instances astropy__astropy-13236

  # Pull images only
  python run_swe_bench.py --test-set quick --pull-images-only

  # Run with custom timeout and workers
  python run_swe_bench.py --test-set small --timeout 3600 --max-workers 2

  # Validate local evaluation environment and exit
  python run_swe_bench.py --validate-env --swe-bench-path /path/to/SWE-bench
        """)

    # Test instance selection
    instance_group = parser.add_mutually_exclusive_group()
    instance_group.add_argument('--test-set',
                               choices=list(TEST_INSTANCE_SETS.keys()),
                               default='small',
                               help='Predefined test instance set')
    instance_group.add_argument('--test-instances',
                               nargs='+',
                               help='Specific test instance IDs')

    # Configuration options
    parser.add_argument('--nano-binary',
                       default='bin/nano',
                       help='Path to nano agent binary')
    parser.add_argument('--dataset',
                       default=DEFAULT_DATASET,
                       help='SWE-bench dataset name')
    parser.add_argument('--timeout',
                       type=int,
                       default=DEFAULT_TIMEOUT,
                       help='Timeout per instance in seconds')
    parser.add_argument('--max-workers',
                       type=int,
                       default=DEFAULT_MAX_WORKERS,
                       help='Maximum parallel workers')
    parser.add_argument('--run-id',
                       help='Custom run ID for evaluation')
    parser.add_argument('--verbose',
                       action='store_true',
                       help='Enable verbose output')
    parser.add_argument('--swe-bench-path',
                       help='Use a locally cloned SWE-bench repository at this path (defaults to env SWE_BENCH_PATH or auto-detected)')

    # Operation modes
    parser.add_argument('--evaluation-only',
                       help='Run evaluation only on a predictions file (path to JSONL)')
    parser.add_argument('--pull-images-only',
                       action='store_true',
                       help='Only pull Docker images without running evaluation')
    parser.add_argument('--skip-build',
                       action='store_true',
                       help='Skip building nano agent binary')
    parser.add_argument('--validate-env',
                       action='store_true',
                       help='Validate local SWE-bench evaluation environment and exit')

    args = parser.parse_args()

    # 确定测试实例
    if args.test_instances:
        test_instances = args.test_instances
    elif args.test_set:
        test_instances = TEST_INSTANCE_SETS[args.test_set]
    else:
        test_instances = TEST_INSTANCE_SETS["small"]  # 默认使用small集合

    # 初始化runner
    runner = SWEBenchRunner(
        project_root=str(Path(__file__).resolve().parent.parent),
        test_dir=str(Path(__file__).resolve().parent),
        nano_binary=args.nano_binary,
        test_instances=test_instances,
        dataset=args.dataset,
        timeout=args.timeout,
        max_workers=args.max_workers,
        run_id=args.run_id,
        verbose=args.verbose,
        swe_bench_path=args.swe_bench_path
    )

    # 仅拉取镜像模式
    if args.pull_images_only:
        ok = runner.pull_images_only()
        sys.exit(0 if ok else 1)

    # 验证环境模式
    if args.validate_env:
        ok = runner.validate_environment()
        sys.exit(0 if ok else 1)

    # Evaluation-only mode
    if args.evaluation_only:
        runner.run_standalone_evaluation(
            predictions_path=args.evaluation_only,
            instance_ids=args.test_instances
        )
        sys.exit(0)

    # 控制是否跳过构建
    runner.skip_build = args.skip_build

    # 运行完整评测流程
    success = runner.run_full_evaluation()
    sys.exit(0 if success else 1)


if __name__ == '__main__':
    main()
