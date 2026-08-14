# Anchor Infer — Full Validation & Submission Plan

A single reference plan covering everything from proving the idea works to submitting on Devpost.

---

## Phase 0 — Setup (before any real work)

1. **Create a public GitHub repository** named something like `anchor-infer` — must be public to get free arm64 GitHub Actions runners.
2. Add a basic README stub, MIT or Apache 2.0 license, and a `.github/workflows/` folder.
3. Confirm you can push to it and that Actions are enabled (Settings → Actions → allow all actions).

---

## Phase 1 — Technical Validation (prove the idea is real)

**Goal:** Confirm, with real numbers, that a KleidiAI-optimized llama.cpp build is measurably faster than a generic build on Arm64 — before building any UI on top.

### Step 1.1 — Set up the arm64 CI workflow
Create `.github/workflows/validate.yml`:
```yaml
name: Validate Arm Optimization
on: [workflow_dispatch, push]
jobs:
  validate:
    runs-on: ubuntu-24.04-arm
    steps:
      - uses: actions/checkout@v4
      - name: Confirm architecture
        run: uname -m   # must print aarch64
      - name: Install build tools
        run: sudo apt-get update && sudo apt-get install -y build-essential cmake git
```
Run it once manually (workflow_dispatch) to confirm the runner is real arm64.

### Step 1.2 — Build the generic (baseline) llama.cpp
- Clone llama.cpp in the workflow
- Build with default/generic flags (no Arm-specific optimization)
- Download a small GGUF-quantized model (1-3B parameters, CPU-friendly)
- Run one test inference to confirm it works at all

### Step 1.3 — Build the KleidiAI-optimized llama.cpp
- Follow Arm's official learning path: "Deploy a LLM chatbot with llama.cpp using KleidiAI on Arm servers"
- Build with KleidiAI kernels enabled
- Check build logs to confirm KleidiAI was actually linked, not silently skipped
- Run the same test inference with the same model

### Step 1.4 — Benchmark both builds
- Use llama.cpp's built-in `llama-bench` with a fixed, identical prompt set for both builds
- Try Arm Performix if time allows for more official/credible numbers
- Record: tokens/sec, time-to-first-token, memory usage, for both builds
- Calculate real percentage improvement — do not round up or estimate

### Step 1.5 — Go/no-go decision
- If you see a real, meaningful improvement (even 15-20%+ is a legitimate story) and the whole loop works end to end → proceed to Phase 2
- If KleidiAI won't build or the improvement is negligible → simplify scope (e.g., try a different model size, or fall back to just quantization as the optimization story) before investing more time

**Checkpoint output:** A markdown file `BENCHMARKS.md` in the repo with the real before/after numbers and how they were produced — this becomes both your validation record and part of your submission's proof of "measurable improvement."

---

## Phase 2 — Automate the pipeline

**Goal:** Turn the manual validation into a repeatable, demoable pipeline — this is the actual codebase judges will look at.

### Step 2.1 — Architecture detection script
- Small Go or Python script/service that runs `uname -m` (or equivalent) at deploy time
- Branches: if arm64 → use KleidiAI-optimized build; else → generic build
- Test by running it explicitly and confirming the correct path is chosen (log the decision clearly)

### Step 2.2 — Wrap the optimized build as a live API
- Use llama.cpp's built-in server mode (`llama-server`) rather than building a custom API wrapper — saves time and is proven
- Confirm you can send a real HTTP request and get a real model response back
- Time the full request round-trip

### Step 2.3 — Bake the benchmark into the deploy flow
- After the model server starts, automatically run the fixed benchmark set
- Output the result in a structured format (JSON) that a dashboard could consume
- This is what makes it "automated," not a one-off manual step — a key differentiator from typical submissions

### Step 2.4 — Minimal dashboard (optional but strongly recommended for WOW factor)
- Lightweight Next.js page: a "Deploy" button, live log stream, and a benchmark comparison card once done
- Doesn't need Anchor's full auth/teams/multi-server system — just enough to demo the flow visually
- This is what turns a benchmark script into a compelling product demo

**Checkpoint output:** A working repo where a single command (or one CI trigger) goes from "nothing running" to "live, benchmarked API endpoint" — ideally visible in a simple UI.

---

## Phase 3 — Documentation (this carries real judging weight)

Judging criteria explicitly reward clear documentation ("Is it clear how to use, run, or validate the project?" — 15 pts) and reusability (20 pts). Don't treat this as an afterthought.

### README must include:
1. **What it does** — one paragraph, plain language
2. **Why it's different** — the "typical submission vs Anchor Infer" comparison table
3. **Architecture diagram** — reuse the flow diagram already built earlier in this conversation, adapted to show: model template → arch detection → optimized build → benchmark → live endpoint
4. **Setup instructions** — exact commands to reproduce your result, tested by literally following them yourself on a clean checkout
5. **Real benchmark numbers** — pull directly from `BENCHMARKS.md`
6. **How to extend it** — brief note on how someone could add a new model template, proving reusability

### Also prepare:
- `BENCHMARKS.md` (from Phase 1) — kept as a standalone, citable artifact
- A short architecture doc or diagram image, not just prose

---

## Phase 4 — Demo preparation

**Goal:** A tight 2-3 minute walkthrough that hits every judging category.

Use the demo narrative already drafted:
1. Brief context: "Anchor is an existing self-hosted deploy platform; this extends it to AI inference" (10 sec)
2. Click deploy on the model template
3. Show live logs detecting arm64 and pulling the KleidiAI build
4. Show the benchmark card appear with a clear percentage improvement
5. Fire a real request at the live endpoint, show a real response
6. Close on reusability: "this same pipeline works for any model template we add next"

**Do a timed dry run at least twice** before submission — this catches dead air, slow builds, or steps that need pre-warming (e.g., pre-pull the model so the demo doesn't sit waiting on a download).

---

## Phase 5 — Submission checklist (Devpost)

Go through each judging category and confirm you have a concrete answer, not just a hope:

- [ ] **Technological Implementation (40 pts)** — Can you point to the exact code that does arm detection, KleidiAI build selection, and benchmarking? Is it in the repo, not just described in the README?
- [ ] **UX/DX (15 pts)** — Can someone unfamiliar clone the repo and reproduce your result following only the README? Test this literally, ideally with a friend who hasn't seen the project.
- [ ] **Potential Impact (20 pts)** — Is there a reusable artifact (the pipeline itself, the workflow file, the benchmark methodology) separate from your one specific demo?
- [ ] **WOW factor (25 pts)** — Does the demo have one unmistakable visual moment (the benchmark number appearing, the live API response) that reads clearly even to someone half-paying-attention?

**Submission materials to prepare:**
- [ ] Public GitHub repo, clean commit history, README complete
- [ ] Demo video (screen recording of the 2-3 min walkthrough)
- [ ] Devpost project page: elevator pitch + project story (already drafted earlier — reuse and adapt for this specific submission)
- [ ] "Built with" tags: go, llama.cpp, kleidiai, arm64, github-actions, nextjs (adjust to what you actually used)
- [ ] Try-it-out links: live demo URL if you deployed one, or clear "run locally" instructions if not
- [ ] Screenshots/GIFs of the dashboard and benchmark card for the Devpost page itself, not just the video

---

## Suggested timeline (compress/expand based on actual hackathon length)

| Phase | What | Time |
|---|---|---|
| 0 | Repo + CI setup | 30 min |
| 1 | Technical validation (both builds + benchmark) | 3-6 hours |
| 2 | Automate pipeline + minimal dashboard | 4-8 hours |
| 3 | Documentation | 1-2 hours |
| 4 | Demo prep + dry runs | 1-2 hours |
| 5 | Submission | 1 hour |

**Priority order if time runs short:** Phase 1 (real numbers) is non-negotiable — without it, there's no submission. Phase 2's dashboard is the first thing to cut/simplify if time is tight; a CLI-based demo with real logs and real numbers still satisfies every judging criterion, just with slightly less visual polish. Phase 3 documentation should never be cut — it's explicitly scored and costs relatively little time.