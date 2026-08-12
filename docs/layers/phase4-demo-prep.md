# Phase 4 — Demo Preparation (Step 7)

A real benchmark takes **20–30 minutes**; a hackathon demo is **3–5 minutes**.
The demo must never wait on a live benchmark. Pre-run the results, then present.

## Pre-demo checklist (the night before or morning of)

1. **Deploy a fresh instance** of the LLM model to the Arm64 server.
2. **Wait for the benchmark to complete** (both generic and optimized runs).
3. **Verify the numbers look correct and representative**:
   - Generation speed: optimized meaningfully higher than generic.
   - Time to first token: optimized lower.
   - Memory: in the expected range; a 4–6 GB difference is normal, not an error.
4. **Leave the deployment running.** Do not tear it down.

## During the presentation

Sections 3 (Live Endpoint) and 4 (Benchmark Card) are already populated with the
pre-run results the moment the page loads (the dashboard restores saved state
from `GET /servers/{id}/infer/status` and `GET /servers/{id}/infer/benchmark`).

### Option A — Live test prompt (guaranteed, recommended)

- The presenter types a prompt in the Section 3 test interface.
- The model responds live — real request, seconds not minutes.
- Judges see a real AI response from a real endpoint, with the real benchmark
  numbers already visible above.

### Option B — Background deploy (higher risk, higher reward)

- Start a new deploy at the beginning of the presentation.
- A **subsequent** deploy skips the model download (weights already on disk) and
  finishes in **3–5 minutes** — live progress fills Section 2 during the talk.
- If timing works out, a live benchmark completion during the presentation is an
  impressive moment. Only attempt if the model is already downloaded and you
  have confidence in the timing.

### Recommendation

**Option A is the guaranteed impressive demo.** Use Option B only as a
deliberate, rehearsed bonus.

## Pre-demo verification commands (5 minutes)

```bash
# 1. Confirm the endpoint answers
curl -s https://infer-<template>.<server>.anchor.app/health

# 2. Confirm benchmark rows exist for the server
#    (dashboard GET /api/v1/servers/<id>/infer/benchmark returns them)
curl -s -H "Authorization: Bearer <token>" \
  https://<control-plane>/api/v1/servers/<id>/infer/benchmark | jq .

# 3. Second-deploy timing check (optional): re-run the deploy from the UI —
#    it should skip the download and complete in minutes.
```

## What to rehearse

- Clicking a model card → Deploy button enables.
- Deploy → Section 2 step list + live log terminal stream.
- Section 3 appears with endpoint URL + API key; copy and reveal work.
- Test interface sends a prompt and shows latency.
- Section 4 benchmark card with the WOW number.
- "Run benchmark again" starts a fresh run (progress + updated card).
