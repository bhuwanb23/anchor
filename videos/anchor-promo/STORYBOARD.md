---
format: 1920x1080
duration: 105s
message: "Own your server — connect, deploy, measure on Arm."
arc: Before → After tease → Bridge → Benefits → How-to connect → Deploy → Infer graphs → CTA
audience: "freelance developers and small agencies babysitting a $5–40 VPS with no sysadmin"
music: "calm, warm, confident, minimal electronic underscore, gentle pulse ~92 BPM, no vocals"
captions: enabled
---

## Video direction

- **Palette system (from frame.md):** warm cream ground `#F6F8FA`; single cobalt `#1F6FEB`; headlines `#0B1B2B`; body muted `#0055D6`. Tinted cards: 4% cobalt fill / 20% cobalt border / 10–14px radius, **no shadows**.
- **Motion:** every frame `entrance → development → settle`. Ease `power3.out` arrivals. Idle budget: ONE living element per frame max.
- **Negative list:** no invented product screenshots or live URLs; no purple AI gradients; no drop shadows; no blanket “Arm is X% faster” — only scoped TinyLlama Q8_0 +15.6%.
- **Caption band:** bottom ~180px reserved (y≥892). Primary content clears that band.
- **Narration:** captions-only + BGM + SFX (no VO track required).

### Verified facts

- Repo: `github.com/bhuwanb23/anchor`
- TinyLlama Q8_0 short: **+15.6%** KleidiAI vs generic on GH `ubuntu-24.04-arm` (see `BENCHMARKS.md`)

---

## Frame 1 — Hook · 0–8s · compositions/frames/01-hook.html

Unchanged. VO: "You own a server…"

## Frame 2 — Pain · 8–17s · compositions/frames/02-pain.html

Unchanged.

## Frame 3 — Tease · 17–25s · compositions/frames/03-tease.html

Unchanged.

## Frame 4 — Mechanism · 25–37s · compositions/frames/04-mechanism.html

Unchanged.

## Frame 5 — Value · 37–49s · compositions/frames/05-value.html

Unchanged.

## Frame 7 — Connect · 49–61s · compositions/frames/07-connect.html

- scene: How-to — seq-connect diagram + three step chips (Name → Install → Connected)
- voiceover: "Name a server. Anchor gives you one install step. When the agent is live, the machine shows connected."
- duration: 12s
- asset_candidates: assets/seq-connect.png
- type: how_to

## Frame 8 — Deploy · 61–73s · compositions/frames/08-deploy.html

- scene: How-to — seq-deploy diagram + chips (Pick image → Ship → Live / plain-English fail)
- voiceover: "Pick an image or an Infer template. Anchor ships it to hardware you own — and tells you if something fails, in plain English."
- duration: 12s
- asset_candidates: assets/seq-deploy.png
- type: how_to

## Frame 9 — Graphs · 73–90s · compositions/frames/09-graphs.html

- scene: Infer pipeline + bench bars; +15.6% count-up; scoped caveat chip
- voiceover: "On Arm we measure, we don't guess. KleidiAI helped TinyLlama Q8 by fifteen percent on our CI runner — other cells near zero."
- duration: 17s
- asset_candidates: assets/infer-pipeline.png, assets/bench-bars.png
- type: proof

## Frame 6 — CTA · 90–105s · compositions/frames/06-cta.html

Moved later. VO: "Bring any five-dollar box. Leave the babysitting to Anchor."
duration: 15s

---

## Pacing notes

- Est. total **105s**.
- Frames 7–9 are the new how-to / proof act; Frame 6 closes.
- BGM padded to 105s with soft out-fade.
