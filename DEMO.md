# Local demo & video guide (Anchor Infer)

Record on **your machine** with a local control plane + dashboard + (optional) Linux agent.
Prefer a pre-warmed Infer deploy so the video never waits on a model download.

## 0. One-time checks

From repo root:

```bash
make tidy
make lint
make test
make build
```

Expected: tidy/test/build succeed. Lint may print React Compiler **warnings** (non-blocking).

## 1. Start local stack

```bash
# Terminal A — API + dashboard
make dev
# Windows opens control-plane in a new window; dashboard: http://localhost:3000
# If the API never responds on :8080, use: make dev-backend-plain

# Terminal B — agent (after you create a server + token)
make dev-agent
```

Copy env if needed:

```bash
cp control-plane/.env.example control-plane/.env
cp agent/.env.example agent/.env
```

**Local agent on Windows:** In the dashboard, **Servers → Add server → Generate**. Copy the **local agent** YAML into `agent/config.yaml` (or set `AGENT_TOKEN` in `agent/.env`). Start **Docker Desktop**, then `make dev-agent`.

## 2. First-run UI (no onboarding wizard)

1. Open http://localhost:3000 → **Sign up** (lands on **Servers** with connect dialog).  
2. **Add server** → generate install command → run on a Linux VPS (Arm64 for Infer story).  
3. Wait until the server shows **connected**.  
4. Go to **Infer** → Detect hardware → Deploy **LLM Chat**.  
5. First deploy: allow HF download (20–45+ min). Leave it running.  
6. Confirm live chat works + benchmark card has numbers.

Morning-of dry run:

```bash
./scripts/demo-prep.sh \
  --control-plane=http://localhost:8080 \
  --token=<access_token from DevTools → Application → localStorage> \
  --server=<server-id>
```

## 3. Video script (2–3 minutes)

| Time | Say / show |
|---|---|
| 0:00–0:15 | “Anchor is a self-hosted ops platform; Infer adds Arm LLM deploy.” |
| 0:15–0:40 | Overview / Servers — agent **connected** on Arm64. |
| 0:40–1:20 | Infer page — Detect → model card → (already deployed) live endpoint. |
| 1:20–1:50 | Type a prompt — real tokens. |
| 1:50–2:20 | Benchmark card — **say the real %** from `BENCHMARKS.md` / CI matrix. |
| 2:20–2:45 | Close: “same pipeline for any template JSON; CI proves KleidiAI when it helps.” |

**Do not** claim a blanket “Arm is X% faster.” Quote cells: TinyLlama Q8_0 **+15.6%**, Q4/Mistral ~**0%** on GH arm64.

## 4. Recording tips

- 1080p, browser zoom 100–110%, hide bookmarks.  
- Wake Render only if demoing hosted UI; local is more reliable for video.  
- Option A (pre-warmed) only — never start a cold HF download on camera.  
- Export MP4 H.264; keep under ~100MB for Devpost if possible.

## 5. After recording

Checklist: [`docs/layers/infer-demo-devpost.md`](docs/layers/infer-demo-devpost.md)  
Judge packet: [`docs/infer-for-judges.md`](docs/infer-for-judges.md)
