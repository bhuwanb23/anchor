# Self-hosted mini-cloud platform — project overview

## One-line pitch
You already have a server. We make it behave like your own private Heroku — deploy in one click, and if anything breaks, we already fixed it or tell you exactly why.

## The problem
Small businesses, freelancers, and dev agencies often run apps on a single cheap VPS or old hardware because AWS/Azure are too expensive or too complex for their scale. But managing that server manually — Docker, SSL certs, backups, monitoring — requires sysadmin skills most of them don't have and don't want to learn.

## The idea, in plain terms
The user brings any compute they already have — a VPS, a rented server, or their own physical hardware. A lightweight agent installed on that server turns it into a self-managing mini-cloud. A web dashboard is the only thing the user ever touches; the agent handles everything else invisibly underneath.

## Who it's for
Small agencies, freelance developers, and small businesses who:
- Already have or can afford a $5-40/month server
- Don't want to hire a sysadmin or learn Linux/Docker/Nginx
- Are currently either paying too much for managed cloud, or manually babysitting their own VPS

## What already exists (and why we're not first)
| Tool | Strength | Gap |
|---|---|---|
| Coolify | Full-featured, active | UI still assumes technical comfort |
| CapRover | Simple, mature | Dated UI, smaller community |
| Dokku | Rock solid | Command-line only, no dashboard |
| Railway/Render | Great UX | You don't own the servers — their cloud, not yours |

The underlying technology (agent + Docker + reverse proxy + dashboard) is a solved problem. We are not trying to out-engineer these tools.

## What's NOT being done well by anyone — our wedge
1. Nobody targets a specific under-served market deeply (local language, local payments, local support hours, templates tuned to what a specific region's small teams actually build).
2. Nobody makes it foolproof for a non-sysadmin — most tools still assume you know what a reverse proxy or SSH key is.
3. Nobody bundles peace-of-mind features (auto backups, plain-English alerts) as a default, not an afterthought.

## What we're building — feature list

**Agent (on the user's server)**
- One-line install, auto-restart on crash, deploy from Git or Docker image, log streaming, health reporting

**Deployment**
- One-click templates (WordPress, Next.js+Postgres, Django+Postgres, static)
- Environment variables, rollback to previous deploy

**Networking**
- Automatic subdomain, custom domain with auto-HTTPS

**Data**
- One-click managed databases (Postgres, MySQL, Redis), persistent storage

**Backup & recovery (differentiator)**
- Automatic daily backups, one-click "restore to yesterday"

**Monitoring & alerts (differentiator)**
- Simple resource dashboard, plain-English alerts (not raw graphs), sent to email/WhatsApp

**Dashboard**
- Signup/login, connect a server, deploy button, logs, team access

## What we are explicitly NOT building
- No custom hypervisor or virtualization layer
- No custom storage clustering (Ceph/Gluster-style) — out of scope, single-server focus
- No auto-scaling, load balancing across regions, or Kubernetes-style orchestration
- No custom container runtime — we use Docker
- No custom TLS/reverse proxy engine — we use Caddy

## How it works (architecture)
1. User connects a server by pasting its IP and running a one-line install script
2. The Go-based agent installs itself, connects securely back to our control plane
3. User deploys an app (Git repo or Docker image) from the dashboard
4. Agent pulls the code/image, runs it in Docker, and Caddy (embedded in the agent) gives it a domain + automatic HTTPS
5. Agent reports health and logs back to the dashboard continuously
6. Agent runs daily backups via restic; user can restore with one click
7. If something breaks, the agent detects it, tries to recover, and sends a plain-English alert

## Tech stack
| Layer | Choice |
|---|---|
| Agent | Go (static binary) |
| Container runtime | Docker |
| Reverse proxy / TLS | Caddy (embedded in agent) |
| Backups | restic |
| Control plane API | Go |
| Database | PostgreSQL |
| Real-time updates | WebSockets |
| Frontend | Next.js + TypeScript + Tailwind |
| Dashboard hosting | Hetzner / DigitalOcean VPS |

## Business model
Usage-based or seat-based SaaS subscription for the control plane. The customer's servers stay theirs — we don't host hardware ourselves, which keeps our costs low and the business easy to scale.

## MVP build order
1. Agent that deploys a single app on one server via CLI (no dashboard yet)
2. Dashboard wrapping that CLI — connect server, deploy, view logs
3. Auth, teams, one-click databases, basic monitoring
4. Billing, multi-server support, backups

## Validation step before writing more code
Talk to 5-10 small agencies/freelancers who currently manage their own VPS manually. Ask what breaks for them today. If "I don't understand why my site went down" and "I'm scared to touch the server" come up often, the wedge is real.