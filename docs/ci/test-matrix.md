# Anchor Infer — Arm bench test matrix

One variable at a time. Headline metric = **generation (tg) tokens/sec**, median of
outer pairs with alternating build order. Always keep the honest Δ% in the pitch.

**Source run:** [Actions #31775408483](https://github.com/bhuwanb23/anchor/actions/runs/31775408483)
(`full-matrix`, `outer_repeats=3`, 2026-08-14). Raw JSON re-parsed locally (CI summary
glob missed `run*-generic.json` — fixed in workflow after this run).

**Go/no-go (per cell):** Δ ≥ ~15% → may support a *scoped* KleidiAI claim for that
model/quant/hardware. Otherwise keep the measure-and-report story.

## Matrix (GH `ubuntu-24.04-arm`)

| Model | Quant | Prompt | Generic tg (median) | KleidiAI tg (median) | Δ% | Range (g / k) | N | Decision |
|---|---|---|---:|---:|---:|---|---:|---|
| TinyLlama 1.1B | Q4_K_M | short (−p128 −n64) | 50.63 | 50.74 | **+0.2%** | 49.9–51.3 / 50.0–51.2 | 3 | **no-go** |
| TinyLlama 1.1B | Q8_0 | short | 56.69 | 65.55 | **+15.6%** | 56.5–57.5 / 64.6–66.4 | 3 | **go (scoped)** |
| Mistral 7B Instruct | Q4_K_M | short | 8.56 | 8.55 | **−0.1%** | 8.52–8.57 / 8.55–8.59 | 3 | **no-go** |
| Mistral 7B Instruct | Q4_K_M | long (−p512 −n128) | 8.63 | 8.64 | **+0.0%** | 8.61–8.64 / 8.56–8.66 | 3 | **no-go** |
| Mistral 7B Instruct | Q4_K_M | short | — | — | — | — | ≥3 | pending — Graviton4/Axion manual |

Also recorded (prompt eval, informational):

| Cell | Generic pp | KleidiAI pp |
|---|---:|---:|
| TinyLlama Q4_K_M short | 147.59 | 147.12 |
| TinyLlama Q8_0 short | 205.23 | 390.39 |
| Mistral Q4_K_M short | 23.11 | 23.10 |
| Mistral Q4_K_M long | 20.30 | 20.28 |

## Decision log

| Date (UTC) | Finding | Submission impact |
|---|---|---|
| 2026-08-14 | TinyLlama Q4_K_M / GH arm64 / short: +0.2% (N=3) | Confirms earlier ~+2.9% single-pair was noise-adjacent; **no** Q4_K_M uplift claim. |
| 2026-08-14 | TinyLlama Q8_0 / GH arm64 / short: **+15.6%** tg (N=3, ranges non-overlapping) | Only CI cell ≥15%. Allow a **scoped** claim: “KleidiAI helped on TinyLlama Q8_0 on this runner (+15.6% tg).” Do not generalize to Q4_K_M or 7B. |
| 2026-08-14 | Mistral 7B Q4_K_M short & long: ~0% | Model-size alone did not open a gap on GH arm64 with Q4_K_M. |
| _pending_ | Same cells on i8mm/SME hardware | Only way to test “CI runner lacks the ISA KleidiAI wants.” |

## Pitch (locked from this matrix)

> Anchor Infer is an automated Arm64 deployment path — architecture detection,
> quantized GGUF, OpenAI-compatible endpoint, and a dual-benchmark card — built to
> surface real KleidiAI gains where hardware and quant support them.
>
> On GitHub `ubuntu-24.04-arm`, measured generation Δ: TinyLlama Q4_K_M **+0.2%**,
> Mistral 7B Q4_K_M **~0%**, TinyLlama Q8_0 **+15.6%**. We report the number; we don’t
> invent one.

**If a judge asks “how much faster?”** — quote the cell above. Never “Arm-optimized,
faster inference” without the figure.
