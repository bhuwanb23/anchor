# Anchor Infer — Arm optimization benchmarks

Phase 1 proof artifact from [`docs/infer_idea_validation.md`](docs/infer_idea_validation.md).
**Living decision tool:** the one-variable test matrix in
[`docs/ci/test-matrix.md`](docs/ci/test-matrix.md) — fill it before locking the submission story.

Numbers come only from real `ubuntu-24.04-arm` runs (or named manual hardware), via
[`.github/workflows/validate.yml`](.github/workflows/validate.yml). Never estimate.

---

## Headline claim (lock only after the matrix says so)

**Current locked claim (after TinyLlama cell only):**

> Anchor Infer is an automated Arm64 deployment path — architecture detection,
> quantized GGUF, OpenAI-compatible endpoint, and a dual-benchmark card, built to
> **surface real KleidiAI gains where hardware and model support them**.

We do **not** claim a large KleidiAI speedup until a matrix cell shows Δ ≥ ~15% with
median-of-N rigor. Until then the honest number for TinyLlama on GH arm64 is **+2.9% tg**.

---

## How to reproduce / extend

1. Actions → **Validate Arm Optimization** → Run workflow.
2. Change **one** input: `model_id` *or* `prompt_profile` *or* hardware (manual).
3. Prefer `outer_repeats=3` (alternating generic/KleidiAI order).
4. Download the artifact → copy the generation Δ row into [`docs/ci/test-matrix.md`](docs/ci/test-matrix.md).

### Local equivalent (aarch64)

```bash
git clone --depth 1 https://github.com/ggml-org/llama.cpp.git && cd llama.cpp
cmake -B build-generic -DGGML_NATIVE=ON -DGGML_CPU_KLEIDIAI=OFF -DLLAMA_CURL=OFF
cmake --build build-generic --config Release -j"$(nproc)" --target llama-bench
cmake -B build-kleidi -DGGML_NATIVE=ON -DGGML_CPU_KLEIDIAI=ON -DLLAMA_CURL=OFF
cmake --build build-kleidi --config Release -j"$(nproc)" --target llama-bench
# Confirm: grep GGML_CPU_KLEIDIAI:BOOL=ON build-kleidi/CMakeCache.txt

# Example — Mistral 7B Q4_K_M (Test 1)
curl -L -o ~/models/model.gguf \
  https://huggingface.co/TheBloke/Mistral-7B-Instruct-v0.2-GGUF/resolve/main/mistral-7b-instruct-v0.2.Q4_K_M.gguf

# Alternate order across pairs; report median tg
./build-generic/bin/llama-bench -m ~/models/model.gguf -p 128 -n 64 -r 3 -o json
./build-kleidi/bin/llama-bench  -m ~/models/model.gguf -p 128 -n 64 -r 3 -o json
```

### Fixed methodology

| Rule | Detail |
|---|---|
| One variable at a time | Model size → quant → hardware → prompt length (see matrix doc) |
| Headline metric | Generation `avg_ts` (tg), not blended pp+tg |
| Stats | Inner `llama-bench -r 3`; outer pairs N=3–5 with **alternating** build order |
| Report | Median tg + range; exact model/quant/hardware/prompt |
| Go/no-go | Per-cell Δ ≥ ~15% before any “faster with KleidiAI” marketing |

---

## Completed cells

### 2026-08-14 — TinyLlama 1.1B Q4_K_M · GH `ubuntu-24.04-arm` · short

| Build | Prompt pp t/s | Generation tg t/s |
|---|---:|---:|
| Generic (`GGML_CPU_KLEIDIAI=OFF`) | 147.54 | 50.58 |
| KleidiAI (`GGML_CPU_KLEIDIAI=ON`) | ~147.5 | 52.04 |

- **Δ tg: +2.9%** (blended median of pp+tg rows was +0.7% — informational only)
- Inner repeats: 3 · Outer pairs: 1 (noise bounds not established)
- llama.cpp: `2bacf9e`
- **Decision:** no-go for uplift claim on this cell

Sidecar: [`docs/ci/arm-benchmark-latest.md`](docs/ci/arm-benchmark-latest.md)

### Pending — next run

**Test 1:** Mistral 7B Instruct Q4_K_M · same runner · short · `outer_repeats=3`  
Dispatch defaults in the workflow already point at this cell.

---

## Relationship to product benchmarks

- This file + the matrix prove the **compiler/runtime** comparison in isolation.
- Dashboard generic vs optimized comes from agent dual-image deploy
  (`deploy_inference` / `run_benchmark`) via `ANCHOR_INFER_IMAGE_BASE`.
- Same honesty rule applies on demo day: show the card’s real %, never invent one.
