# Anchor Infer — Arm bench test matrix

One variable at a time. Fill cells from [`validate.yml`](../../.github/workflows/validate.yml)
artifacts (`ci-benchmark-results.json`). Headline metric = **generation (tg) tokens/sec**,
median across outer pairs; always keep the honest Δ% visible in the pitch.

**Go/no-go (per cell):** Δ ≥ ~15% → may support a KleidiAI uplift claim for that
model/hardware/quant. Otherwise keep the **measure-and-report pipeline** story.

## How to run

**Full CI matrix in parallel** (recommended):

Actions → **Validate Arm Optimization** → Run workflow → suite = `full-matrix`, outer_repeats = `3`.

That spins **4 parallel arm64 jobs**:

| Job | model_id | prompt |
|---|---|---|
| 1 | `tinyllama-q4km` | short |
| 2 | `tinyllama-q8_0` | short |
| 3 | `mistral-7b-q4km` | short |
| 4 | `mistral-7b-q4km` | long |

Other suites: `smoke` (TinyLlama only), `mistral-only`, `tinyllama-quants`.

Test 3 (Graviton4 / Axion / i8mm·SME) stays **manual outside CI**.

## Matrix

| Model | Quant | Hardware | Prompt | Generic tg (median) | KleidiAI tg (median) | Δ% | N pairs | Status |
|---|---|---|---|---:|---:|---:|---:|---|
| TinyLlama 1.1B | Q4_K_M | GH `ubuntu-24.04-arm` | short (−p128 −n64) | 50.58 | 52.04 | **+2.9%** | 1* | done — no-go (pre-matrix) |
| TinyLlama 1.1B | Q4_K_M | GH `ubuntu-24.04-arm` | short | — | — | — | 3 | pending — full-matrix |
| TinyLlama 1.1B | Q8_0 | GH `ubuntu-24.04-arm` | short | — | — | — | 3 | pending — full-matrix |
| Mistral 7B Instruct | Q4_K_M | GH `ubuntu-24.04-arm` | short | — | — | — | 3 | pending — full-matrix |
| Mistral 7B Instruct | Q4_K_M | GH `ubuntu-24.04-arm` | long (−p512 −n128) | — | — | — | 3 | pending — full-matrix |
| Mistral 7B Instruct | Q4_K_M | Graviton4 / Axion (manual) | short | — | — | — | ≥3 | pending |

\*Legacy single-pair cell from 2026-08-14. Replaced by N=3 when full-matrix finishes.

## Decision log

| Date (UTC) | Cell | Finding | Submission impact |
|---|---|---|---|
| 2026-08-14 | TinyLlama Q4_K_M / GH arm64 / short | +2.9% tg (single pair) | Do **not** claim large KleidiAI speedups. Pitch = measure-and-report Arm deploy path. |
| _pending_ | Mistral 7B Q4_K_M / GH arm64 / short | | If Δ still &lt;15%, Test 2/3 next; if ≥15%, may add a scoped “on 7B / this runner” claim with the real number. |

## Pitch rule

If a judge asks “how much faster?”: answer with the **filled cell** (model, hardware,
median tg, Δ%, N). Never substitute “Arm-optimized, faster inference” for a missing
or modest number. A well-instrumented marginal result beats a vague inflated claim.
