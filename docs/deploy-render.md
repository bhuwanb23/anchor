# Deploy Anchor to Render (free tier)

Anchor’s **cloud half** (control plane + dashboard) runs on Render’s **free** web services. The **agent** stays on each customer’s Linux VPS.

## Architecture

| Service | Render type | Role |
|---------|-------------|------|
| `anchor-api` | Free Web Service (Docker) | Go control plane, SQLite, WebSockets, `/install.sh` |
| `anchor-web` | Free Web Service (Docker) | Next.js dashboard |

Default URLs ([`render.yaml`](../render.yaml)):

- API: `https://anchor-api.onrender.com`
- Dashboard: `https://anchor-web.onrender.com`

## Free-tier realities

| Topic | What happens |
|-------|----------------|
| Cost | $0 — both services use `plan: free` |
| Sleep | Idle ~15 minutes → instance spins down |
| Cold start | First request after sleep takes ~1 minute (loading page) |
| Disk | **Not available** on free — no persistent volume |
| SQLite | Lives at `/tmp/anchor/…` and is **wiped** on sleep, restart, or redeploy |
| JWT secret | Survives (stored in Render env); user **accounts/data do not** |
| WebSockets | Work while awake; agents/browsers reconnect after wake |

Use free for demos and trying the UI. For durable accounts + always-on agent links, upgrade `anchor-api` to **Starter** and attach a disk at `/data` (see [Upgrade path](#upgrade-path-paid)).

## Prerequisites

1. [Render](https://render.com) account
2. This repo on GitHub/GitLab
3. Patience for cold starts after idle

## Deploy (Blueprint)

1. Push the repo (including `render.yaml`).
2. Render → **New → Blueprint** → select the repo.
3. Confirm `anchor-api` and `anchor-web` (both Free).
4. Apply and wait until **Live**.
5. Open the dashboard URL. If the API is asleep, the first login may take ~1 minute — retry once.

If service names are taken, rename them in `render.yaml` and update every `*.onrender.com` value.

## Environment reference

### `anchor-api`

| Variable | Free default | Purpose |
|----------|--------------|---------|
| `JWT_SECRET` | auto-generated | Auth signing (persists in Render) |
| `DATABASE_PATH` | `/tmp/anchor/yourplatform.db` | Ephemeral SQLite |
| `FRONTEND_URL` | `https://anchor-web.onrender.com` | CORS |
| `PUBLIC_BASE_URL` | `https://anchor-api.onrender.com` | Install + agent WS URLs |

### `anchor-web` (build-time)

| Variable | Purpose |
|----------|---------|
| `NEXT_PUBLIC_API_URL` | REST base baked into the client |
| `NEXT_PUBLIC_WS_URL` | Browser WebSocket (`wss://…/ws/browser`) |

After changing `NEXT_PUBLIC_*`, **clear build cache and redeploy** `anchor-web`.

## Smoke checklist

- [ ] Hit `https://anchor-api.onrender.com/health` (may cold-start once)
- [ ] Dashboard loads; sign up / login works
- [ ] Header shows **Live** after API is awake
- [ ] Registration install command uses `https://` + your API host
- [ ] Expect: after ~15 min idle, reopen the app → cold start; **re-register** if the DB was wiped

## Upgrade path (paid)

When you need data + agents to stay connected:

1. Change `anchor-api` to `plan: starter` in the Dashboard (or Blueprint).
2. Attach a **persistent disk** mounted at `/data`.
3. Set:

```text
DATABASE_PATH=/data/yourplatform.db
DB_BACKUP_DIR=/data/backups
```

4. Redeploy `anchor-api`. Keep `anchor-web` on free if you want — only the API needs the disk for SQLite.

Optional: enable S3 DB backups (`S3_*` + `DB_BACKUP_ENCRYPTION_KEY`) so you have an off-box copy even without a disk.

## Agent install note

`/install.sh` is served from the API. Binaries under `/releases/` appear only after you publish into `release/` (`make release`). Until then, install may fail at download even if the API is healthy.

On free, agents will disconnect when the API sleeps and reconnect after wake (cold start delay).

## Local Docker (optional)

```bash
# from repo root
docker build -f control-plane/Dockerfile -t anchor-api .
docker run --rm -p 8080:8080 \
  -e JWT_SECRET=dev -e FRONTEND_URL=http://localhost:3000 \
  -e PUBLIC_BASE_URL=http://localhost:8080 \
  -e DATABASE_PATH=/tmp/anchor/yourplatform.db \
  anchor-api

docker build -f dashboard/Dockerfile \
  --build-arg NEXT_PUBLIC_API_URL=http://localhost:8080 \
  --build-arg NEXT_PUBLIC_WS_URL=ws://localhost:8080/ws/browser \
  -t anchor-web .
docker run --rm -p 3000:3000 anchor-web
```

### If `anchor-web` build looks “stuck” on npm

Render’s builders often log `[WARN] Request took 10–30s: https://registry.npmjs.org/...` while resolving packages. That is **slowness**, not an immediate failure. The dashboard Dockerfile raises pnpm fetch timeouts/retries and uses `pnpm fetch` + offline install so the build survives slow registry RTTs. Clear the build cache and redeploy if a prior install timed out mid-way.

## Non-goals

- Agent / Docker / Caddy on Render
- Free persistent SQLite (Render does not allow disks on free)
- Multi-instance API
