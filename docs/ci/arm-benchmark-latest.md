# CI benchmark run — 2026-08-14T05:37:44Z

- Runner: `ubuntu-24.04-arm` (`aarch64`)
- Model: TinyLlama 1.1B Chat Q4_K_M
- Prompt tokens: 128 · generation: 64 · repeats: 3
- llama.cpp: `2bacf9ea5cf9e191430d4c786202d5984df7c193`

| Build | Blended median t/s (pp+tg) | Generation `avg_ts` | Source |
|---|---:|---:|---|
| Generic (`GGML_CPU_KLEIDIAI=OFF`) | 99.06 | 50.58 | json |
| KleidiAI (`GGML_CPU_KLEIDIAI=ON`) | 99.77 | 52.04 | json |

**Blended improvement:** 0.7% (99.77 vs 99.06 t/s)  
**Generation improvement:** 2.9% (52.04 vs 50.58 t/s)

**Decision:** below 15–20% bar → narrow product claims to Arm path + quantization (see `BENCHMARKS.md`).
