# Anchor Infer — Arm optimization benchmarks

This file is the **Phase 1 proof artifact** from [`docs/infer_idea_validation.md`](docs/infer_idea_validation.md):
measurable tokens/sec improvement of a KleidiAI-enabled llama.cpp build vs a generic Arm build.

Numbers below must come from a real `ubuntu-24.04-arm` run (GitHub Actions workflow
[`.github/workflows/validate.yml`](.github/workflows/validate.yml)) — never estimated.

---

## How to reproduce

1. Ensure Actions can use **arm64** runners (`ubuntu-24.04-arm`) on this repository.
2. Run **Actions → Validate Arm Optimization → Run workflow** (or push a change under `infer/` / the workflow file).
3. Download the `arm-benchmark-results` artifact.
4. Copy the median tokens/sec lines into **Latest CI results** below (keep prior rows in history if useful).

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

./build-generic/bin/llama-bench -m ~/models/model.gguf -p 128 -n 64 -r 3
./build-kleidi/bin/llama-bench  -m ~/models/model.gguf -p 128 -n 64 -r 3
```

### Fixed parameters

| Parameter | Value |
|---|---|
| Runner | GitHub `ubuntu-24.04-arm` (`aarch64`) |
| Model | TinyLlama 1.1B Chat v1.0 Q4_K_M |
| Prompt tokens (`-p`) | 128 |
| Generation tokens (`-n`) | 64 |
| Repeats (`-r`) | 3 |
| Metric | Median tokens/sec from `llama-bench` |

---

## Latest CI results

| Date (UTC) | Generic t/s | KleidiAI t/s | Improvement | llama.cpp / notes |
|---|---:|---:|---:|---|
| _pending first workflow run_ | — | — | — | Fill from `ci-benchmark-results.md` artifact |

**Go / no-go rule:** keep the KleidiAI product story if improvement is real and meaningful (about **15–20%+**). If improvement is negligible, narrow README claims to “Arm path + quantization” before demo day.

---

## Relationship to Anchor product benchmarks

- This file proves the **compiler/runtime** claim (KleidiAI vs generic) in isolation.
- The dashboard **generic vs optimized** card comes from the agent dual-image deploy path
  (`deploy_inference` / `run_benchmark`) using tags from `ANCHOR_INFER_IMAGE_BASE`.
- Both should agree directionally once Infer images are built with KleidiAI (`infer/docker/`).
