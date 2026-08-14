# Anchor Infer — Arm optimization benchmarks

This file is the **Phase 1 proof artifact** from [`docs/infer_idea_validation.md`](docs/infer_idea_validation.md):
measurable tokens/sec of a KleidiAI-enabled llama.cpp build vs a generic Arm build.

Numbers below come from a real `ubuntu-24.04-arm` run (GitHub Actions workflow
[`.github/workflows/validate.yml`](.github/workflows/validate.yml)) — never estimated.

---

## How to reproduce

1. Ensure Actions can use **arm64** runners (`ubuntu-24.04-arm`) on this repository.
2. Run **Actions → Validate Arm Optimization → Run workflow** (or push a change under `infer/` / the workflow file).
3. Download the `arm-benchmark-results` artifact.
4. Copy the tokens/sec lines into **Latest CI results** below (keep prior rows in history if useful).

### Local equivalent (on an aarch64 Linux box)

```bash
git clone --depth 1 https://github.com/ggml-org/llama.cpp.git && cd llama.cpp

# Generic
cmake -B build-generic -DGGML_NATIVE=ON -DGGML_CPU_KLEIDIAI=OFF -DLLAMA_CURL=OFF
cmake --build build-generic --config Release -j"$(nproc)" --target llama-bench

# KleidiAI
cmake -B build-kleidi -DGGML_NATIVE=ON -DGGML_CPU_KLEIDIAI=ON -DLLAMA_CURL=OFF
cmake --build build-kleidi --config Release -j"$(nproc)" --target llama-bench
# Confirm: grep GGML_CPU_KLEIDIAI:BOOL=ON build-kleidi/CMakeCache.txt

# Model (TinyLlama 1.1B Q4_K_M)
mkdir -p ~/models
curl -L -o ~/models/model.gguf \
  https://huggingface.co/TheBloke/TinyLlama-1.1B-Chat-v1.0-GGUF/resolve/main/tinyllama-1.1b-chat-v1.0.Q4_K_M.gguf

./build-generic/bin/llama-bench -m ~/models/model.gguf -p 128 -n 64 -r 3 -o json
./build-kleidi/bin/llama-bench  -m ~/models/model.gguf -p 128 -n 64 -r 3 -o json
```

### Fixed parameters

| Parameter | Value |
|---|---|
| Runner | GitHub `ubuntu-24.04-arm` (`aarch64`) |
| Model | TinyLlama 1.1B Chat v1.0 Q4_K_M |
| Prompt tokens (`-p`) | 128 |
| Generation tokens (`-n`) | 64 |
| Repeats (`-r`) | 3 |
| Threads | 4 (runner default in this run) |
| Metric | `llama-bench` `avg_ts` (tokens/sec), prompt vs generation reported separately |

---

## Latest CI results

| Date (UTC) | Generic pp t/s | Generic tg t/s | KleidiAI pp t/s | KleidiAI tg t/s | tg Δ | llama.cpp |
|---|---:|---:|---:|---:|---:|---|
| 2026-08-14 | 147.54 | 50.58 | ~147.5* | 52.04 | **+2.9%** | `2bacf9e` |

\*KleidiAI JSON from this run exposed one clear generation row at **52.04 t/s**; CI’s blended median across pp+tg rows was **99.77 vs 99.06 t/s (+0.7%)**. Headline claim uses **generation (tg)** only.

### Go / no-go decision (2026-08-14)

**No-go for a KleidiAI uplift story.** Measured improvement is **~0.7% blended / ~2.9% generation** on this runner + TinyLlama Q4_K_M — far below the ~15–20% bar in the validation plan.

**Product claim going forward:** Anchor Infer is an **Arm64 deploy path** (arch detect → quantized GGUF → OpenAI-compatible endpoint → dual bench card). Do **not** market “large KleidiAI speedups” until a later run on hardware with i8mm/SME (or a heavier model) shows a real gap. Keep building with `GGML_CPU_KLEIDIAI=ON` where supported; treat it as best-effort, not the headline.

Sidecar copy of the CI summary: [`docs/ci/arm-benchmark-latest.md`](docs/ci/arm-benchmark-latest.md).

---

## Relationship to Anchor product benchmarks

- This file proves the **compiler/runtime** comparison (KleidiAI vs generic) in isolation.
- The dashboard **generic vs optimized** card comes from the agent dual-image deploy path
  (`deploy_inference` / `run_benchmark`) using tags from `ANCHOR_INFER_IMAGE_BASE`.
- Both should agree directionally once Infer images are built with KleidiAI (`infer/docker/`).
  Expect small deltas on GitHub’s shared arm64 runners; larger deltas need featureful Arm CPUs.
