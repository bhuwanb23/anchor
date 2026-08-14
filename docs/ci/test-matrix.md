# Anchor Infer — Arm bench test matrix

One variable at a time. Fill cells from [`validate.yml`](../../.github/workflows/validate.yml)
artifacts (`ci-benchmark-results.json`). Headline metric = **generation (tg) tokens/sec**,
median across outer pairs; always keep the honest Δ% visible in the pitch.

**Go/no-go (per cell):** Δ ≥ ~15% → may support a KleidiAI uplift claim for that
model/hardware/quant. Otherwise keep the **measure-and-report pipeline** story.

## How to run the next cell

Actions → **Validate Arm Optimization** → Run workflow:

| Goal | `model_id` | `prompt_profile` | `outer_repeats` |
|---|---|---|---|
| Test 1 — model size | `mistral-7b-q4km` | `short` | `3` |
| Test 4 — long prompt | same model as last good cell | `long` | `3` |
| Test 2 — quant | `tinyllama-q8_0` or `mistral-7b-q8_0` | `short` | `3` |
| Baseline re-check | `tinyllama-q4km` | `short` | `3` |

Test 3 (Graviton4 / Axion / i8mm·SME) is **manual outside CI** — same `llama-bench`
flags, paste into the matrix with hardware = that instance.

## Matrix

| Model | Quant | Hardware | Prompt | Generic tg (median) | KleidiAI tg (median) | Δ% | N pairs | Status |
|---|---|---|---|---:|---:|---:|---:|---|
| TinyLlama 1.1B | Q4_K_M | GH `ubuntu-24.04-arm` | short (−p128 −n64) | 50.58 | 52.04 | **+2.9%** | 1* | done — no-go |
| Mistral 7B Instruct | Q4_K_M | GH `ubuntu-24.04-arm` | short | — | — | — | 3 | **next — run via workflow_dispatch** |
| Mistral 7B Instruct | Q4_K_M | GH `ubuntu-24.04-arm` | long (−p512 −n128) | — | — | — | 3 | pending |
| TinyLlama 1.1B | Q8_0 | GH `ubuntu-24.04-arm` | short | — | — | — | 3 | pending |
| Mistral 7B Instruct | Q4_K_M | Graviton4 / Axion (manual) | short | — | — | — | ≥3 | pending |

\*First cell used a single outer pair (`llama-bench -r 3` inner only).  
**Note (2026-08-14):** Actions run [#4](https://github.com/bhuwanb23/anchor/actions/runs/31774666953) succeeded but was a **push** with defaults → artifact `arm-benchmark-tinyllama-q4km-short` only (~5m). That does **not** fill the Mistral row. Test 1 requires **Run workflow** (dispatch), not a docs push.

## Decision log

| Date (UTC) | Cell | Finding | Submission impact |
|---|---|---|---|
| 2026-08-14 | TinyLlama Q4_K_M / GH arm64 / short | +2.9% tg (single pair) | Do **not** claim large KleidiAI speedups. Pitch = measure-and-report Arm deploy path. |
| _pending_ | Mistral 7B Q4_K_M / GH arm64 / short | | If Δ still &lt;15%, Test 2/3 next; if ≥15%, may add a scoped “on 7B / this runner” claim with the real number. |

## Pitch rule

If a judge asks “how much faster?”: answer with the **filled cell** (model, hardware,
median tg, Δ%, N). Never substitute “Arm-optimized, faster inference” for a missing
or modest number. A well-instrumented marginal result beats a vague inflated claim.
