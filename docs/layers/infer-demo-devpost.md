# Anchor Infer — Demo & Devpost checklist

Companion to [`docs/infer_idea_validation.md`](../infer_idea_validation.md) Phase 4–5.
Product dry-run notes: [`phase4-demo-prep.md`](phase4-demo-prep.md).
Honest numbers: [`../ci/test-matrix.md`](../ci/test-matrix.md) · [`../../BENCHMARKS.md`](../../BENCHMARKS.md).

## Elevator pitch (use this wording)

> Anchor Infer is an automated Arm64 deployment path — architecture detection,
> quantized GGUF, an OpenAI-compatible endpoint, and a dual-benchmark card — built to
> surface real KleidiAI gains where hardware supports them, and to show the real
> measured delta when it does not.

**If asked “how much faster?”** — quote the matrix cell (e.g. TinyLlama on GH arm64:
**+2.9%** generation t/s). Do not say “Arm-optimized, faster inference” without the
number. Judges respect a well-instrumented marginal result more than a vague claim.

## Timed dry run (2–3 minutes)

Prefer **Option A** (pre-warmed): model already downloaded, benchmark already persisted;
live moment is the chat prompt.

1. Open `/infer` with a connected Arm agent (10s context).
2. Show readiness card (Arm microarchitecture + optimization label).
3. If not pre-deployed: click Deploy and show live progress (or skip if restored).
4. Show live endpoint URL + reveal API key (Section 3).
5. Send one test prompt — real tokens appear.
6. Point at benchmark card — **say the real %** (and that CI matrix is how we keep measuring).
7. Close on reusability: “new model = new template JSON, same measure-and-report pipeline.”

Run `./scripts/demo-prep.sh` the morning of the demo.

## Devpost submission checklist

### Judging categories

- [ ] **Technological Implementation (40)** — Point judges at:
  - `agent/internal/platform/` (Arm detect + image tag selection)
  - `agent/internal/executor/deploy_inference.go` + `infer/benchmark.go`
  - `.github/workflows/validate.yml` + `BENCHMARKS.md` + `docs/ci/test-matrix.md`
- [ ] **UX/DX (15)** — Stranger can follow README Infer section + reproduce a matrix cell.
- [ ] **Potential Impact (20)** — Reusable artifacts: workflow inputs (model/quant/prompt),
  matrix methodology, `infer/docker/`, template JSON.
- [ ] **WOW (25)** — Live chat response + honest benchmark card (real Δ, not marketing fluff).

### Materials

- [ ] Public GitHub repo (arm64 Actions enabled)
- [ ] README Infer section uses measure-and-report pitch (not inflated speedup)
- [ ] `BENCHMARKS.md` + matrix: TinyLlama cell filled; Mistral 7B cell after next CI run
- [ ] Demo video (2–3 min) — say the real % out loud
- [ ] Devpost page: elevator pitch above + project story
- [ ] Built with tags: go, llama.cpp, kleidiai, arm64, github-actions, nextjs
- [ ] Try-it-out: live dashboard URL and/or local run instructions
- [ ] Screenshots/GIFs of `/infer` + benchmark card

## Operator commands (live product path)

```bash
# Images
./infer/docker/build.sh ghcr.io/<you>/anchor-infer --push

# Agent host
export ANCHOR_INFER_IMAGE_BASE=ghcr.io/<you>/anchor-infer

# API smoke
./scripts/validate-infer-product.sh \
  --control-plane=https://… --token=… --server=…

# Demo morning
./scripts/demo-prep.sh \
  --control-plane=https://… --token=… --server=… \
  --endpoint=https://infer-…
```

## Next CI cell (Test 1)

Actions → Validate Arm Optimization → Run workflow with defaults:

- `model_id`: `mistral-7b-q4km`
- `prompt_profile`: `short`
- `outer_repeats`: `3`

Paste results into `docs/ci/test-matrix.md` before changing the pitch.
