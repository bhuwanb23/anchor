# CI full-matrix — 2026-08-14 (run 31775408483)

- Runner: `ubuntu-24.04-arm` · outer pairs: 3 · alternating order
- llama.cpp: `3d93885`

| Cell | Generic tg | KleidiAI tg | Δ% | Decision |
|---|---:|---:|---:|---|
| TinyLlama Q4_K_M short | 50.63 | 50.74 | +0.2% | no-go |
| TinyLlama Q8_0 short | 56.69 | 65.55 | **+15.6%** | go (scoped) |
| Mistral 7B Q4_K_M short | 8.56 | 8.55 | −0.1% | no-go |
| Mistral 7B Q4_K_M long | 8.63 | 8.64 | +0.0% | no-go |

See `docs/ci/test-matrix.md` for ranges and pitch wording.
