# SWE-bench Evaluation Report

[中文](./SWE_BENCH.zh-CN.md)

This document records nano-agent's evaluation on the [SWE-bench](https://github.com/princeton-nlp/SWE-bench) benchmark, which tests the ability to resolve real-world GitHub issues.

## Scope and honest caveats

- **This is a 31-instance subset evaluation, not the full SWE-bench Verified 500-instance set.** The default dataset of the evaluation harness is `princeton-nlp/SWE-bench_Verified` (see `swe_bench_test/run_swe_bench.py`), and the 31 instances below were selected from it.
- The reported 61.3% success rate therefore reflects this subset only and is **not directly comparable** to full-set SWE-bench Verified leaderboard numbers.
- Results were recorded as of the initial public release (commit `5736b9c`, 2026-04-20) and have been carried in the README since then.
- Task-level detail (per-instance durations, token usage, attempt logs) was not captured for this run. **Full task-level detail will be added with the next complete evaluation.**

## Latest test results

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

**Performance summary:**

- **Success rate**: 61.3% (19/31 issues resolved)
- **Completion rate**: 100% (31/31 instances completed)
- **Zero errors**: no failed executions or empty patches

**Resolved instances (19 total):**

| Instance | Project |
|----------|---------|
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

**Unresolved instances (12 total):**

| Instance | Project |
|----------|---------|
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

## Methodology

- **Execution mode**: binary `swebench` mode. The compiled nano-agent binary is placed inside a per-instance Docker container and driven non-interactively against the issue statement.
- **Environment**: each instance runs in a pre-built SWE-bench evaluation image (`swe-bench.eval.x86_64.{instance_id}`) with the repository checked out at `/testbed`, matching the official evaluation conditions.
- **Patch extraction**: nano-agent writes its fix as `solution.patch` in the container output directory; predictions are collected into `predictions.jsonl`.
- **Judging**: resolution is determined by the official `swebench` evaluation harness — an instance counts as *resolved* only if the patch passes the instance's `FAIL_TO_PASS` tests without breaking its `PASS_TO_PASS` tests. No manual or LLM-based grading is involved.

See [`swe_bench_test/README.md`](../../swe_bench_test/README.md) for the full harness description.

## Reproducing the evaluation

```bash
cd swe_bench_test

# Install dependencies (the script can also bootstrap a venv automatically)
pip install -r requirements.txt

# Set LLM credentials
export NANO_API_KEY="your-api-key"

# Evaluate specific instances
python run_swe_bench.py --test-instances astropy__astropy-13236 django__django-12345

# Or use a predefined test set
python run_swe_bench.py --test-set small
```

Key options: `--dataset` (default `princeton-nlp/SWE-bench_Verified`), `--timeout`, `--max-workers`, `--pull-images-only`, `--skip-build`. See `python run_swe_bench.py --help` and `swe_bench_test/README.md` for details.

## Future work

- Run the **full SWE-bench Verified 500-instance set** and publish per-instance duration/cost detail.
- Add **pass^k repeated-run statistics** to report variance, not just a single pass@1 number.

Progress is tracked in [ROADMAP.md](../../ROADMAP.md).

## References

- [SWE-bench official repository](https://github.com/princeton-nlp/SWE-bench)
- [SWE-bench Verified](https://openai.com/index/introducing-swe-bench-verified/) (500 human-validated instances)
