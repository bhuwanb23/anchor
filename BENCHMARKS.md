# Anchor Infer — Arm optimization benchmarks

Phase 1 proof artifact. Living decision tool:
[`docs/ci/test-matrix.md`](docs/ci/test-matrix.md).

Numbers from real `ubuntu-24.04-arm` runs via
[`.github/workflows/validate.yml`](.github/workflows/validate.yml) — never estimated.

**Latest full matrix:** [Actions run 31775408483](https://github.com/bhuwanb23/anchor/actions/runs/31775408483)
(2026-08-14, 4 parallel cells, N=3 outer pairs, alternating order).

---

## Headline claim

> Anchor Infer is an automated Arm64 deployment path — architecture detection,
> quantized GGUF, OpenAI-compatible endpoint, and a dual-benchmark card — built to
> **surface real KleidiAI gains where model/quant/hardware support them**.

On GH arm64 we measured:

| Cell | Generation Δ (median of 3) |
|---|---|
| TinyLlama Q4_K_M short | **+0.2%** |
| TinyLlama Q8_0 short | **+15.6%** ← only cell ≥15% |
| Mistral 7B Q4_K_M short | **−0.1%** |
| Mistral 7B Q4_K_M long | **+0.0%** |

Scoped claim allowed: KleidiAI helped **TinyLlama Q8_0** on this runner. Do **not**
claim a general KleidiAI speedup for Q4_K_M or 7B on GH Actions arm64.

---

## Full matrix results (2026-08-14)

| Model | Quant | Prompt | Generic tg | KleidiAI tg | Δ% | Decision |
|---|---|---|---:|---:|---:|---|
| TinyLlama 1.1B | Q4_K_M | short | 50.63 | 50.74 | +0.2% | no-go |
| TinyLlama 1.1B | Q8_0 | short | 56.69 | 65.55 | **+15.6%** | go (scoped) |
| Mistral 7B Instruct | Q4_K_M | short | 8.56 | 8.55 | −0.1% | no-go |
| Mistral 7B Instruct | Q4_K_M | long | 8.63 | 8.64 | +0.0% | no-go |

Ranges (tg): TinyLlama Q4 49.9–51.3 vs 50.0–51.2 · Q8 56.5–57.5 vs 64.6–66.4 ·
Mistral short 8.52–8.57 vs 8.55–8.59 · long 8.61–8.64 vs 8.56–8.66.

llama.cpp commit on that run: `3d93885`.

---

## Methodology

| Rule | Detail |
|---|---|
| Parallel cells | `suite=full-matrix` → one arm64 job per cell |
| Headline metric | Generation `avg_ts` (tg), not blended pp+tg |
| Stats | Inner `llama-bench -r 3`; outer pairs N=3 with alternating order |
| Go/no-go | Per-cell Δ ≥ ~15% before any uplift marketing |

Reproduce: Actions → Validate Arm Optimization → `full-matrix` / `outer_repeats=3`.

---

## Relationship to product benchmarks

Dashboard generic vs optimized comes from agent dual-image deploy. Same honesty rule
on demo day: show the card’s real %, quote the matrix cell if asked.
