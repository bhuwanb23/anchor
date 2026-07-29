# YourPlatform

A management layer for self-hosted servers. Bring your own compute — own the hardware, get a managed experience.

## Quick Start

```bash
# Start everything (backend + frontend)
make dev

# Or start individually
make dev-backend    # Control plane on :8080 (hot reload)
make dev-frontend   # Dashboard on :3000 (hot reload)
make dev-agent      # Agent (requires Docker)
```

## Prerequisites

- **Go** 1.26+
- **Node.js** 24+
- **pnpm** (for dashboard)
- **Docker** (for agent)
- **make** (GnuWin32 on Windows, or via Homebrew/apt)

## Architecture

```
┌─────────────┐     WebSocket      ┌───────────────┐
│  Dashboard  │◄──── REST API ────►│ Control Plane │
│  (Next.js)  │     :3000          │   (Go) :8080  │
└─────────────┘                    └───────┬───────┘
                                           │
                                     WebSocket
                                           │
                                    ┌──────▼──────┐
                                    │    Agent     │
                                    │   (Go)      │
                                    └──────┬──────┘
                                           │
                                      Docker API
                                           │
                                    ┌──────▼──────┐
                                    │  App Containers │
                                    └─────────────┘
```

## Project Structure

```
yourplatform/
├── agent/                  # Runs on managed servers
│   ├── cmd/agent/main.go
│   └── internal/
│       ├── config/         # YAML + env config
│       ├── preflight/      # Environment checks
│       ├── docker/         # Docker SDK client
│       ├── caddy/          # Reverse proxy management
│       ├── backup/         # Restic backup manager
│       ├── ws/             # WebSocket client
│       └── executor/       # Command execution
│
├── control-plane/          # Central API server
│   ├── cmd/server/main.go
│   └── internal/
│       ├── api/            # HTTP handlers + middleware
│       ├── db/             # SQLite + migrations
│       ├── ws/             # WebSocket hub
│       ├── auth/           # JWT + password hashing
│       └── config/         # Env-based config
│
├── dashboard/              # Web UI
│   ├── app/                # Next.js App Router pages
│   ├── components/         # React components
│   ├── lib/                # API client, auth, WebSocket
│   ├── hooks/              # React hooks
│   └── types/              # TypeScript types
│
├── scripts/                # Utility scripts
├── docs/                   # Documentation
├── Makefile                # One-command dev workflows
└── README.md
```

## API Endpoints

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| GET | `/health` | No | Health check |
| POST | `/api/v1/auth/register` | No | Create account |
| POST | `/api/v1/auth/login` | No | Sign in |
| GET | `/api/v1/auth/me` | Yes | Get current user |
| GET | `/api/v1/servers` | Yes | List servers |
| POST | `/api/v1/servers` | Yes | Create server |
| POST | `/api/v1/deploy` | Yes | Deploy app |
| GET | `/api/v1/deployments` | Yes | Get deployments |

## Make Targets

```
make dev              Start all services (backend + frontend)
make dev-backend      Start control plane with hot reload
make dev-agent        Build and run agent locally
make dev-frontend     Start Next.js dev server

make build            Build all binaries + frontend
make build-backend    Build control plane binary
make build-agent      Build agent binary
make build-frontend   Build Next.js for production

make tidy             Tidy Go modules
make lint             Run linters (go vet + eslint)
make test             Run all tests
make db-reset         Delete SQLite database (fresh start)
make clean            Remove build artifacts
make help             Show all available targets
```

## Development

1. Clone the repo
2. Copy `.env.example` to `.env` in `control-plane/`
3. Run `make dev-backend` in one terminal
4. Run `make dev-frontend` in another terminal
5. Open http://localhost:3000

## Configuration

### Control Plane (`.env`)

```env
PORT=8080
ENV=development
JWT_SECRET=your-secret-here
DATABASE_PATH=./yourplatform.db
FRONTEND_URL=http://localhost:3000
```

### Agent (`config.yaml`)

```yaml
control_plane_url: ws://localhost:8080/ws/agent
agent_token: <token-from-server-creation>
server_id: <server-id>
docker_socket: unix:///var/run/docker.sock
```

## Tech Stack

- **Backend**: Go, Chi router, SQLite, WebSocket
- **Frontend**: Next.js 16, TypeScript, Tailwind CSS
- **Agent**: Go, Docker SDK, WebSocket
- **Infra**: Caddy (reverse proxy), Restic (backups)
