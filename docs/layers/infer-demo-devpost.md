# Anchor Infer — Demo & Devpost checklist

Companion to [`docs/infer_idea_validation.md`](../infer_idea_validation.md) Phase 4–5.
Product dry-run notes also live in [`phase4-demo-prep.md`](phase4-demo-prep.md).

## Timed dry run (2–3 minutes)

Prefer **Option A** (pre-warmed): model already downloaded, benchmark already persisted;
live moment is the chat prompt.

1. Open `/infer` with a connected Arm agent (10s context).
2. Show readiness card (Arm microarchitecture + optimization label).
3. If not pre-deployed: click Deploy and show live progress (or skip if restored).
4. Show live endpoint URL + reveal API key (Section 3).
5. Send one test prompt — real tokens appear.
6. Point at benchmark card (% faster optimized vs generic).
7. Close on reusability: “new model = new template JSON, same pipeline.”

Run `./scripts/demo-prep.sh` the morning of the demo.

## Devpost submission checklist

### Judging categories

- [ ] **Technological Implementation (40)** — Point judges at:
  - `agent/internal/platform/` (Arm detect + image tag selection)
  - `agent/internal/executor/deploy_inference.go` + `infer/benchmark.go`
  - `.github/workflows/validate.yml` + `BENCHMARKS.md`
- [ ] **UX/DX (15)** — Stranger can follow README Infer section + `BENCHMARKS.md` reproduce steps.
- [ ] **Potential Impact (20)** — Reusable artifacts: workflow, `infer/docker/`, template JSON, benchmark methodology.
- [ ] **WOW (25)** — One clear visual: benchmark % and/or live chat response.

### Materials

- [ ] Public GitHub repo (arm64 Actions enabled)
- [ ] README Infer section complete (done in root README)
- [ ] `BENCHMARKS.md` filled with first CI numbers (after workflow run)
- [ ] Demo video (2–3 min screen recording of dry run)
- [ ] Devpost page: elevator pitch + story
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
