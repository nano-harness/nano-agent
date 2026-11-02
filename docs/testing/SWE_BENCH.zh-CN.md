# SWE-bench 评测报告

[English](./SWE_BENCH.md)

本文档记录 nano-agent 在 [SWE-bench](https://github.com/princeton-nlp/SWE-bench) 基准测试上的评测结果，该基准测试评估解决真实世界 GitHub 问题的能力。

## 范围与诚实口径

- **这是 31 个实例的子集评测，不是 SWE-bench Verified 完整 500 题。** 评测脚本的默认数据集为 `princeton-nlp/SWE-bench_Verified`（见 `swe_bench_test/run_swe_bench.py`），以下 31 个实例从中选取。
- 因此，报告的 61.3% 成功率仅反映该子集，**不可直接对标** SWE-bench Verified 完整榜单上的数字。
- 结果记录于首次公开发布（commit `5736b9c`，2026-04-20），此后一直保留在 README 中。
- 本次运行未采集任务级明细（单实例耗时、token 用量、尝试日志）。**完整的任务级明细将随下一次完整评测补充。**

## 最新测试结果

```json
{
    "total_instances": 31,
    "submitted_instances": 31,
    "completed_instances": 31,
    "resolved_instances": 19,
    "unresolved_instances": 12,
    "empty_patch_instances": 0,
    "error_instances": 0,
    "success_rate": "61.3%"
}
```

**性能摘要：**

- **成功率**：61.3%（19/31 个问题已解决）
- **完成率**：100%（31/31 个实例已完成）
- **零错误**：没有失败的执行或空补丁

**已解决实例（共 19 个）：**

| 实例 | 项目 |
|------|------|
| `django__django-10880` | django |
| `django__django-10914` | django |
| `django__django-11133` | django |
| `matplotlib__matplotlib-13989` | matplotlib |
| `matplotlib__matplotlib-14623` | matplotlib |
| `matplotlib__matplotlib-23314` | matplotlib |
| `matplotlib__matplotlib-24149` | matplotlib |
| `matplotlib__matplotlib-25311` | matplotlib |
| `pydata__xarray-2905` | xarray |
| `pydata__xarray-3095` | xarray |
| `pytest-dev__pytest-5262` | pytest |
| `pytest-dev__pytest-5631` | pytest |
| `scikit-learn__scikit-learn-10297` | scikit-learn |
| `scikit-learn__scikit-learn-10844` | scikit-learn |
| `scikit-learn__scikit-learn-10908` | scikit-learn |
| `sympy__sympy-11618` | sympy |
| `sympy__sympy-12096` | sympy |
| `sympy__sympy-12419` | sympy |
| `sympy__sympy-20590` | sympy |

**未解决实例（共 12 个）：**

| 实例 | 项目 |
|------|------|
| `astropy__astropy-12907` | astropy |
| `astropy__astropy-13033` | astropy |
| `astropy__astropy-13236` | astropy |
| `astropy__astropy-14365` | astropy |
| `astropy__astropy-14995` | astropy |
| `django__django-10097` | django |
| `django__django-10554` | django |
| `django__django-11179` | django |
| `matplotlib__matplotlib-20488` | matplotlib |
| `psf__requests-2317` | requests |
| `sphinx-doc__sphinx-10323` | sphinx |
| `sphinx-doc__sphinx-10435` | sphinx |

## 方法论

- **执行模式**：binary `swebench` 模式。编译好的 nano-agent 二进制被放入每个实例独立的 Docker 容器中，以非交互方式处理问题描述。
- **环境**：每个实例运行在预构建的 SWE-bench 评测镜像（`swe-bench.eval.x86_64.{instance_id}`）中，仓库检出在 `/testbed`，与官方评测条件一致。
- **补丁提取**：nano-agent 将修复写入容器输出目录的 `solution.patch`；所有预测汇总到 `predictions.jsonl`。
- **判定方式**：由官方 `swebench` 评测框架判定——只有当补丁通过该实例的 `FAIL_TO_PASS` 测试且不破坏 `PASS_TO_PASS` 测试时，实例才计为*已解决*。不涉及任何人工或 LLM 评分。

评测脚本的完整说明见 [`swe_bench_test/README.md`](../../swe_bench_test/README.md)。

## 复现评测

```bash
cd swe_bench_test

# 安装依赖（脚本也可以自动创建 venv）
pip install -r requirements.txt

# 设置 LLM 凭据
export NANO_API_KEY="your-api-key"

# 评测指定实例
python run_swe_bench.py --test-instances astropy__astropy-13236 django__django-12345

# 或使用预定义测试集
python run_swe_bench.py --test-set small
```

主要选项：`--dataset`（默认 `princeton-nlp/SWE-bench_Verified`）、`--timeout`、`--max-workers`、`--pull-images-only`、`--skip-build`。详见 `python run_swe_bench.py --help` 与 `swe_bench_test/README.md`。

## 后续计划

- 运行 **SWE-bench Verified 完整 500 题**，并公布单实例耗时/成本明细。
- 增加 **pass^k 多次重复运行统计**，报告方差而非单一的 pass@1 数字。

进展见 [ROADMAP.md](../../ROADMAP.md)。

## 参考资料

- [SWE-bench 官方仓库](https://github.com/princeton-nlp/SWE-bench)
- [SWE-bench Verified](https://openai.com/index/introducing-swe-bench-verified/)（500 个人工验证实例）
