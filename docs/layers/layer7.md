# Layer 7 — Frontend Dashboard: Complete Plan

---

## What Layer 7 Actually Is

```
Layer 7 is the only thing the user ever sees.

Every other layer exists to make Layer 7 work.
The agent, the control plane, the WebSocket hub —
all of it is invisible infrastructure.

Layer 7 is the product.

The user judges the entire system by what they see here.
A technically perfect backend with a confusing dashboard
is a failed product.

Two design mandates that override everything else:

Mandate 1: A non-technical user must be able to
           go from "I have a VPS" to "my app is live"
           without googling anything.

Mandate 2: The day-to-day view must communicate
           "everything is fine" or "here is exactly
           what is wrong and what to do" at a glance.
```

---

## The Two Zones

```
Zone 1: Onboarding
  Linear, wizard-style, one decision at a time.
  User goes through it once (maybe twice lifetime).
  Goal: zero confusion, massive "it worked" moment.

  Steps:
  1. Create account
  2. Connect your server (copy one command, paste it)
  3. Deploy your first app (pick a template or paste an image)
  4. See it live with HTTPS

Zone 2: Day-to-day
  Non-linear, calm, overview-first.
  User comes here every day (or gets alerted and comes here).
  Goal: understand server health in 5 seconds.
       Take action in 2 clicks.

  Contains:
  → Server overview (health gauges, status)
  → App cards (status, domain, quick actions)
  → Logs viewer
  → Alert center
  → Deployment history
  → Backup status
```

---

## Step 1 — Project Structure

### Folder Layout

```
dashboard/
├── app/
│   ├── layout.tsx                    ← root layout (fonts, providers)
│   ├── page.tsx                      ← root redirect
│   │
│   ├── (auth)/                       ← auth pages (no sidebar)
│   │   ├── layout.tsx                ← centered card layout
│   │   ├── login/
│   │   │   └── page.tsx
│   │   ├── register/
│   │   │   └── page.tsx
│   │   ├── forgot-password/
│   │   │   └── page.tsx
│   │   └── reset-password/
│   │       └── page.tsx
│   │
│   ├── onboarding/                   ← Zone 1: wizard flow
│   │   ├── layout.tsx                ← minimal layout (no nav)
│   │   ├── page.tsx                  ← redirect to first step
│   │   ├── connect-server/
│   │   │   └── page.tsx             ← step 1: paste install command
│   │   └── first-deploy/
│   │       └── page.tsx             ← step 2: deploy first app
│   │
│   └── dashboard/                    ← Zone 2: day-to-day
│       ├── layout.tsx                ← sidebar + header layout
│       ├── page.tsx                  ← overview (all servers)
│       └── servers/
│           └── [server_id]/
│               ├── page.tsx          ← server overview
│               ├── apps/
│               │   ├── page.tsx      ← app list
│               │   ├── new/
│               │   │   └── page.tsx  ← deploy new app
│               │   └── [app_id]/
│               │       ├── page.tsx  ← app detail
│               │       └── logs/
│               │           └── page.tsx ← log viewer
│               ├── backups/
│               │   └── page.tsx
│               ├── alerts/
│               │   └── page.tsx
│               └── settings/
│                   └── page.tsx
│
├── components/
│   ├── ui/                           ← base components
│   │   ├── button.tsx
│   │   ├── input.tsx
│   │   ├── card.tsx
│   │   ├── badge.tsx
│   │   ├── dialog.tsx
│   │   ├── toast.tsx
│   │   ├── skeleton.tsx
│   │   └── progress.tsx
│   │
│   ├── layout/                       ← structural components
│   │   ├── sidebar.tsx
│   │   ├── header.tsx
│   │   └── page-header.tsx
│   │
│   ├── server/                       ← server-specific components
│   │   ├── server-card.tsx
│   │   ├── server-status-badge.tsx
│   │   ├── metrics-gauges.tsx
│   │   └── connection-status.tsx
│   │
│   ├── app/                          ← app-specific components
│   │   ├── app-card.tsx
│   │   ├── deploy-button.tsx
│   │   ├── deployment-status.tsx
│   │   └── env-var-list.tsx
│   │
│   ├── logs/                         ← log viewer components
│   │   ├── log-viewer.tsx
│   │   └── log-line.tsx
│   │
│   └── alerts/                       ← alert components
│       ├── alert-card.tsx
│       └── alert-banner.tsx
│
├── lib/
│   ├── api.ts                        ← axios instance + interceptors
│   ├── auth.ts                       ← token management
│   ├── ws.ts                         ← WebSocket client
│   └── utils.ts                      ← helpers (formatting, etc.)
│
├── hooks/
│   ├── use-auth.ts                   ← auth state
│   ├── use-server.ts                 ← server data
│   ├── use-metrics.ts               ← real-time metrics
│   ├── use-logs.ts                   ← log streaming
│   └── use-command.ts               ← command tracking
│
├── store/
│   ├── auth-store.ts                 ← zustand auth store
│   ├── server-store.ts               ← zustand server store
│   └── ws-store.ts                   ← websocket state store
│
├── types/
│   └── index.ts                      ← all TypeScript types
│
└── middleware.ts                     ← Next.js route protection
```

---

## Step 2 — Types and API Client

### Step 2A — TypeScript Types

```typescript
// types/index.ts

export interface User {
  id: string
  email: string
  name: string
  created_at: string
}

export interface AuthResponse {
  access_token: string
  refresh_token: string
  token_type: string
  expires_in: number
  user: User
}

export type ServerStatus =
  | 'pending'
  | 'connected'
  | 'disconnected'
  | 'updating'
  | 'error'

export interface ServerMetrics {
  cpu_percent: number
  ram_used_mb: number
  ram_total_mb: number
  ram_percent: number
  disk_used_gb: number
  disk_total_gb: number
  disk_percent: number
  load_1min: number
  recorded_at: string
}

export interface Server {
  id: string
  name: string
  status: ServerStatus
  public_ip: string
  agent_version: string
  last_seen: string
  os: string
  os_version: string
  cpu_cores: number
  ram_total_mb: number
  disk_total_gb: number
  created_at: string
  metrics: ServerMetrics | null
}

export type AppStatus =
  | 'deploying'
  | 'running'
  | 'stopped'
  | 'failed'
  | 'removing'

export interface App {
  id: string
  server_id: string
  project_name: string
  status: AppStatus
  current_image: string | null
  platform_domain: string | null
  custom_domains: string[]
  memory_limit_mb: number
  app_port: number
  created_at: string
  updated_at: string
}

export type DeploymentStatus =
  | 'pending'
  | 'in_progress'
  | 'success'
  | 'failed'
  | 'rolled_back'

export interface Deployment {
  id: string
  image: string
  status: DeploymentStatus
  error: string | null
  started_at: string | null
  completed_at: string | null
  duration_ms: number | null
  triggered_by: { id: string; name: string } | null
  domain: string | null
}

export interface ContainerState {
  project: string
  role: string
  status: string
  health: string | null
  cpu_percent: number
  ram_used_mb: number
  ram_limit_mb: number
  restart_count: number
}

export type AlertSeverity = 'warning' | 'critical'
export type AlertStatus = 'active' | 'resolved' | 'acknowledged'

export interface Alert {
  id: string
  server_id: string
  project_name: string | null
  severity: AlertSeverity
  type: string
  status: AlertStatus
  title: string
  message: string
  detail: string | null
  action: string | null
  fired_at: string
  resolved_at: string | null
}

export interface Backup {
  id: string
  status: 'running' | 'success' | 'partial' | 'failed'
  size_new_bytes: number | null
  size_total_bytes: number | null
  verified: boolean
  started_at: string
  completed_at: string | null
  duration_seconds: number | null
  project_results: Array<{
    project: string
    status: string
    error?: string
  }> | null
}

export interface LogLine {
  timestamp: string
  stream: 'stdout' | 'stderr'
  text: string
}

export interface Command {
  id: string
  type: string
  status: string
  result: unknown | null
  error: string | null
  issued_at: string
  completed_at: string | null
}

// WebSocket message types
export type WsMessageType =
  | 'connected'
  | 'server_update'
  | 'server_state'
  | 'agent_connected'
  | 'agent_disconnected'
  | 'command_ack'
  | 'command_progress'
  | 'command_result'
  | 'command_queued'
  | 'initial_logs'
  | 'log_lines'
  | 'stream_ended'
  | 'alert'
  | 'ping'

export interface WsMessage {
  type: WsMessageType
  [key: string]: unknown
}

export interface ServerUpdateMessage {
  type: 'server_update'
  server_id: string
  timestamp: string
  metrics: ServerMetrics
  containers: ContainerState[]
  alerts: Alert[]
}

export interface CommandProgressMessage {
  type: 'command_progress'
  command_id: string
  step: string
  step_number: number
  total_steps: number
  message: string
  percent: number
}

export interface CommandResultMessage {
  type: 'command_result'
  command_id: string
  status: 'success' | 'failed' | 'timeout'
  result: unknown
  error: string | null
}
```

### Step 2B — API Client

```typescript
// lib/api.ts

import axios, {
  AxiosInstance,
  InternalAxiosRequestConfig,
  AxiosResponse
} from 'axios'

const BASE_URL = process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:8080'

const api: AxiosInstance = axios.create({
  baseURL: BASE_URL,
  headers: { 'Content-Type': 'application/json' },
  timeout: 30000,
})

// Attach JWT to every request
api.interceptors.request.use(
  (config: InternalAxiosRequestConfig) => {
    const token = getAccessToken()
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }
    return config
  }
)

// Handle token expiry and refresh
let isRefreshing = false
let refreshQueue: Array<(token: string) => void> = []

api.interceptors.response.use(
  (response: AxiosResponse) => response,
  async (error) => {
    const originalRequest = error.config

    // Token expired and this is not a retry
    if (
      error.response?.status === 401 &&
      error.response?.headers?.['x-token-expired'] === 'true' &&
      !originalRequest._retry
    ) {
      originalRequest._retry = true

      if (isRefreshing) {
        // Another request already refreshing — wait for it
        return new Promise((resolve) => {
          refreshQueue.push((newToken: string) => {
            originalRequest.headers.Authorization = `Bearer ${newToken}`
            resolve(api(originalRequest))
          })
        })
      }

      isRefreshing = true

      try {
        const newToken = await refreshAccessToken()
        refreshQueue.forEach((cb) => cb(newToken))
        refreshQueue = []
        originalRequest.headers.Authorization = `Bearer ${newToken}`
        return api(originalRequest)
      } catch {
        // Refresh failed — log out
        clearTokens()
        window.location.href = '/login'
        return Promise.reject(error)
      } finally {
        isRefreshing = false
      }
    }

    // Non-401 or already retried — reject
    return Promise.reject(error)
  }
)

// Token management
function getAccessToken(): string | null {
  if (typeof window === 'undefined') return null
  return localStorage.getItem('access_token')
}

function getRefreshToken(): string | null {
  if (typeof window === 'undefined') return null
  return localStorage.getItem('refresh_token')
}

export function setTokens(accessToken: string, refreshToken: string): void {
  localStorage.setItem('access_token', accessToken)
  localStorage.setItem('refresh_token', refreshToken)
}

export function clearTokens(): void {
  localStorage.removeItem('access_token')
  localStorage.removeItem('refresh_token')
}

async function refreshAccessToken(): Promise<string> {
  const refreshToken = getRefreshToken()
  if (!refreshToken) throw new Error('No refresh token')

  const response = await axios.post(`${BASE_URL}/api/v1/auth/refresh`, {
    refresh_token: refreshToken,
  })

  const { access_token } = response.data
  localStorage.setItem('access_token', access_token)
  return access_token
}

export default api
```

### Step 2C — WebSocket Client

```typescript
// lib/ws.ts

import { WsMessage } from '@/types'

type MessageHandler = (message: WsMessage) => void
type StatusHandler = (status: 'connecting' | 'connected' | 'disconnected') => void

class WebSocketClient {
  private ws: WebSocket | null = null
  private url: string
  private handlers: Map<string, Set<MessageHandler>> = new Map()
  private statusHandlers: Set<StatusHandler> = new Set()
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private reconnectAttempts = 0
  private maxReconnectDelay = 60000
  private shouldReconnect = true
  private pingInterval: ReturnType<typeof setInterval> | null = null
  private pongTimeout: ReturnType<typeof setTimeout> | null = null

  constructor(url: string) {
    this.url = url
  }

  connect(token: string): void {
    this.shouldReconnect = true
    this.connectWithToken(token)
  }

  private connectWithToken(token: string): void {
    const url = `${this.url}?token=${encodeURIComponent(token)}`
    this.notifyStatus('connecting')

    try {
      this.ws = new WebSocket(url)
    } catch {
      this.scheduleReconnect(token)
      return
    }

    this.ws.onopen = () => {
      this.reconnectAttempts = 0
      this.notifyStatus('connected')
      this.startPingInterval()
    }

    this.ws.onmessage = (event: MessageEvent) => {
      try {
        const message: WsMessage = JSON.parse(event.data as string)
        this.dispatch(message)
      } catch {
        // Malformed message — ignore
      }
    }

    this.ws.onclose = () => {
      this.stopPingInterval()
      this.notifyStatus('disconnected')
      if (this.shouldReconnect) {
        this.scheduleReconnect(token)
      }
    }

    this.ws.onerror = () => {
      // onclose fires after onerror — handle reconnect there
    }
  }

  private scheduleReconnect(token: string): void {
    const delay = Math.min(
      1000 * Math.pow(2, this.reconnectAttempts),
      this.maxReconnectDelay
    )
    const jitter = Math.random() * delay * 0.5
    const totalDelay = delay + jitter

    this.reconnectAttempts++
    this.reconnectTimer = setTimeout(() => {
      this.connectWithToken(token)
    }, totalDelay)
  }

  private startPingInterval(): void {
    this.pingInterval = setInterval(() => {
      this.send({ type: 'ping' })

      // Expect pong within 10 seconds
      this.pongTimeout = setTimeout(() => {
        // No pong — connection is dead
        this.ws?.close()
      }, 10000)
    }, 30000)
  }

  private stopPingInterval(): void {
    if (this.pingInterval) {
      clearInterval(this.pingInterval)
      this.pingInterval = null
    }
    if (this.pongTimeout) {
      clearTimeout(this.pongTimeout)
      this.pongTimeout = null
    }
  }

  send(message: object): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(message))
    }
  }

  subscribe(server_id: string): void {
    this.send({ type: 'subscribe', server_id })
  }

  unsubscribe(server_id: string): void {
    this.send({ type: 'unsubscribe', server_id })
  }

  on(type: string, handler: MessageHandler): () => void {
    if (!this.handlers.has(type)) {
      this.handlers.set(type, new Set())
    }
    this.handlers.get(type)!.add(handler)

    // Return cleanup function
    return () => {
      this.handlers.get(type)?.delete(handler)
    }
  }

  onStatus(handler: StatusHandler): () => void {
    this.statusHandlers.add(handler)
    return () => this.statusHandlers.delete(handler)
  }

  private dispatch(message: WsMessage): void {
    // Handle pong — clear timeout
    if (message.type === 'pong') {
      if (this.pongTimeout) {
        clearTimeout(this.pongTimeout)
        this.pongTimeout = null
      }
      return
    }

    const handlers = this.handlers.get(message.type)
    if (handlers) {
      handlers.forEach((handler) => handler(message))
    }

    // Also dispatch to wildcard handlers
    const wildcardHandlers = this.handlers.get('*')
    if (wildcardHandlers) {
      wildcardHandlers.forEach((handler) => handler(message))
    }
  }

  private notifyStatus(status: 'connecting' | 'connected' | 'disconnected'): void {
    this.statusHandlers.forEach((handler) => handler(status))
  }

  disconnect(): void {
    this.shouldReconnect = false
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
    }
    this.stopPingInterval()
    this.ws?.close()
  }
}

// Singleton instance
let wsClient: WebSocketClient | null = null

export function getWsClient(): WebSocketClient {
  if (!wsClient) {
    const url = process.env.NEXT_PUBLIC_WS_URL ?? 'ws://localhost:8080'
    wsClient = new WebSocketClient(`${url}/ws/browser`)
  }
  return wsClient
}
```

### Done Condition for Step 2
```
□ All TypeScript types defined and consistent with Layer 6 API
□ API client attaches JWT to every request
□ Token refresh happens automatically on 401
□ Refresh queue prevents multiple simultaneous refresh calls
□ Failed refresh redirects to login
□ WebSocket client reconnects with exponential backoff
□ WebSocket client sends pings and detects dead connections
□ Event handlers can be added and removed cleanly
□ No TypeScript errors in types file
```

---

## Step 3 — State Management

### Step 3A — Auth Store

```typescript
// store/auth-store.ts

import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { User } from '@/types'
import { setTokens, clearTokens } from '@/lib/api'

interface AuthState {
  user: User | null
  isAuthenticated: boolean
  isLoading: boolean

  setUser: (user: User) => void
  login: (user: User, accessToken: string, refreshToken: string) => void
  logout: () => void
  setLoading: (loading: boolean) => void
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      user: null,
      isAuthenticated: false,
      isLoading: true,

      setUser: (user) => set({ user, isAuthenticated: true }),

      login: (user, accessToken, refreshToken) => {
        setTokens(accessToken, refreshToken)
        set({ user, isAuthenticated: true, isLoading: false })
      },

      logout: () => {
        clearTokens()
        set({ user: null, isAuthenticated: false })
      },

      setLoading: (isLoading) => set({ isLoading }),
    }),
    {
      name: 'auth-store',
      partialize: (state) => ({ user: state.user }),
      // Only persist user object, not loading/auth state
      // Tokens are in localStorage separately
    }
  )
)
```

### Step 3B — Server Store

```typescript
// store/server-store.ts

import { create } from 'zustand'
import { Server, ContainerState, Alert, ServerMetrics } from '@/types'

interface ServerState {
  servers: Server[]
  selectedServerId: string | null

  // Real-time data for the selected server
  metrics: ServerMetrics | null
  containers: ContainerState[]
  activeAlerts: Alert[]

  setServers: (servers: Server[]) => void
  updateServer: (serverId: string, updates: Partial<Server>) => void
  setSelectedServer: (serverId: string | null) => void
  updateMetrics: (metrics: ServerMetrics) => void
  updateContainers: (containers: ContainerState[]) => void
  addAlert: (alert: Alert) => void
  resolveAlert: (alertId: string) => void
}

export const useServerStore = create<ServerState>((set) => ({
  servers: [],
  selectedServerId: null,
  metrics: null,
  containers: [],
  activeAlerts: [],

  setServers: (servers) => set({ servers }),

  updateServer: (serverId, updates) =>
    set((state) => ({
      servers: state.servers.map((s) =>
        s.id === serverId ? { ...s, ...updates } : s
      ),
    })),

  setSelectedServer: (serverId) =>
    set({
      selectedServerId: serverId,
      metrics: null,        // clear stale data
      containers: [],
      activeAlerts: [],
    }),

  updateMetrics: (metrics) => set({ metrics }),

  updateContainers: (containers) => set({ containers }),

  addAlert: (alert) =>
    set((state) => ({
      activeAlerts: [alert, ...state.activeAlerts].slice(0, 50),
    })),

  resolveAlert: (alertId) =>
    set((state) => ({
      activeAlerts: state.activeAlerts.filter((a) => a.id !== alertId),
    })),
}))
```

### Step 3C — WebSocket Store

```typescript
// store/ws-store.ts

import { create } from 'zustand'

type WsStatus = 'connecting' | 'connected' | 'disconnected'

interface CommandProgress {
  commandId: string
  step: string
  message: string
  percent: number
  stepNumber: number
  totalSteps: number
}

interface WsState {
  status: WsStatus
  activeCommands: Map<string, CommandProgress>

  setStatus: (status: WsStatus) => void
  setCommandProgress: (commandId: string, progress: CommandProgress) => void
  clearCommand: (commandId: string) => void
}

export const useWsStore = create<WsState>((set) => ({
  status: 'disconnected',
  activeCommands: new Map(),

  setStatus: (status) => set({ status }),

  setCommandProgress: (commandId, progress) =>
    set((state) => {
      const next = new Map(state.activeCommands)
      next.set(commandId, progress)
      return { activeCommands: next }
    }),

  clearCommand: (commandId) =>
    set((state) => {
      const next = new Map(state.activeCommands)
      next.delete(commandId)
      return { activeCommands: next }
    }),
}))
```

### Done Condition for Step 3
```
□ Auth store persists user across page refresh
□ Auth store does not persist tokens (they are in localStorage directly)
□ Server store clears stale data when selected server changes
□ WebSocket store tracks in-progress commands
□ Stores are accessible from any component without prop drilling
□ No circular dependencies between stores
```

---

## Step 4 — Route Protection

```typescript
// middleware.ts

import { NextResponse } from 'next/server'
import type { NextRequest } from 'next/server'

const PUBLIC_PATHS = [
  '/login',
  '/register',
  '/forgot-password',
  '/reset-password',
]

export function middleware(request: NextRequest): NextResponse {
  const { pathname } = request.nextUrl
  const isPublicPath = PUBLIC_PATHS.some((path) =>
    pathname.startsWith(path)
  )

  // Access token stored in localStorage — cannot read in middleware
  // Use a lightweight cookie-based indicator instead
  const hasSession = request.cookies.has('has_session')

  if (!hasSession && !isPublicPath) {
    const loginUrl = new URL('/login', request.url)
    loginUrl.searchParams.set('next', pathname)
    return NextResponse.redirect(loginUrl)
  }

  if (hasSession && isPublicPath) {
    return NextResponse.redirect(new URL('/dashboard', request.url))
  }

  return NextResponse.next()
}

export const config = {
  matcher: ['/((?!api|_next/static|_next/image|favicon.ico).*)'],
}
```

```
Note on the has_session cookie:
  → Set when user logs in: a session indicator cookie (no sensitive data)
  → Cleared on logout
  → HttpOnly: false (Next.js middleware can read it)
  → The actual JWT stays in localStorage (accessible to JS)
  → The cookie is just a signal: "user has logged in before"
  → This pattern avoids sending the JWT to the server on every navigation

Why this approach:
  → JWT in cookies requires CSRF protection (complex)
  → JWT in localStorage requires JS to check auth (cannot do in middleware)
  → Cookie indicator is a clean middle ground for Next.js App Router
```

---

## Step 5 — Zone 1: Onboarding Flow

### Step 5A — Onboarding Layout

```
The onboarding layout is intentionally minimal.
No sidebar. No navigation. No distractions.
Just the current step and a progress indicator.

Layout structure:
┌────────────────────────────────────────────┐
│  YourPlatform                              │
│                                            │
│  ●────────────●────────────○              │
│  Connect        Deploy       Done          │
│  Server         App                        │
│                                            │
│  ┌──────────────────────────────────────┐  │
│  │                                      │  │
│  │          Current step content        │  │
│  │                                      │  │
│  └──────────────────────────────────────┘  │
│                                            │
│              [Back]  [Continue]            │
└────────────────────────────────────────────┘
```

### Step 5B — Step 1: Connect Server

```
Goal: get the user's server connected.

What the user sees:
┌────────────────────────────────────────────────────────┐
│                                                        │
│  Connect your server                                   │
│                                                        │
│  Run this command on your server to connect it.        │
│  You'll only do this once.                             │
│                                                        │
│  ┌──────────────────────────────────────────────────┐  │
│  │                                                  │  │
│  │  curl -fsSL https://get.yourplatform.com/        │  │
│  │  install.sh | sudo sh -s --                      │  │
│  │  --token=reg_a1b2c3d4e5f6...                     │  │
│  │                                                  │  │
│  │                                    [Copy] ✓      │  │
│  └──────────────────────────────────────────────────┘  │
│                                                        │
│  Token expires in: 47 minutes                          │
│                                                        │
│  ┌──────────────────────────────────────────────────┐  │
│  │  ⏳  Waiting for your server to connect...       │  │
│  │     This page will update automatically.         │  │
│  └──────────────────────────────────────────────────┘  │
│                                                        │
│  Need help? → How to open a terminal on your server   │
└────────────────────────────────────────────────────────┘

Behavior:
  1. Page loads → API call: GET /api/v1/servers/{id}/registration-token
     → Displays the install command
     → Shows token expiry countdown

  2. WebSocket connected → subscribes to this server's events
     → Listens for: agent_connected event

  3. On agent_connected:
     → Stop showing the waiting spinner
     → Brief celebration: "✓ Server connected!"
     → Auto-advance to Step 2 after 1.5 seconds

  4. Token expires:
     → Show: "Your token has expired. Generate a new one."
     → Button: "Generate new command"
     → Calls API to get fresh token, updates display

What NOT to show:
  → SSH instructions (the user pasted the command, that is enough)
  → Technical details about what the command does
  → Multiple options or configuration
  → Linux tutorials
```

### Step 5C — Step 2: Deploy First App

```
Goal: get the user's first app running.

What the user sees:
┌────────────────────────────────────────────────────────┐
│                                                        │
│  Deploy your first app                                 │
│                                                        │
│  Choose a template or use your own Docker image.       │
│                                                        │
│  Quick templates:                                      │
│  ┌──────────────┐  ┌──────────────┐  ┌─────────────┐  │
│  │  📝           │  │  🛒           │  │  🐍          │  │
│  │  Next.js     │  │  WordPress   │  │  Django     │  │
│  │  + Postgres  │  │              │  │  + Postgres │  │
│  │              │  │              │  │             │  │
│  │  [Use this]  │  │  [Use this]  │  │  [Use this] │  │
│  └──────────────┘  └──────────────┘  └─────────────┘  │
│                                                        │
│  ────────────── or use your own ──────────────         │
│                                                        │
│  Docker image: [nginx:latest              ]            │
│  App name:     [my-app                    ]            │
│  Port:         [3000                      ]            │
│                                                        │
│                              [Deploy →]                │
└────────────────────────────────────────────────────────┘

When user clicks Deploy:
┌────────────────────────────────────────────────────────┐
│                                                        │
│  Deploying my-app...                                   │
│                                                        │
│  ✓  Pulling nginx:latest                               │
│  ✓  Setting up networking                              │
│  ⏳  Starting your app...                              │
│  ○  Configuring HTTPS                                  │
│                                                        │
│  ██████████████████░░░░░░░░░░  65%                     │
│                                                        │
│  This usually takes 30-90 seconds.                     │
└────────────────────────────────────────────────────────┘

When deploy succeeds:
┌────────────────────────────────────────────────────────┐
│                                                        │
│  🎉 Your app is live!                                  │
│                                                        │
│  my-app is running at:                                 │
│  https://my-app.srv-abc.yourplatform.app               │
│                                      [Open ↗]          │
│                                                        │
│  Daily backups: ✓ enabled                              │
│  HTTPS: ✓ automatic                                    │
│  Auto-restart: ✓ enabled                               │
│                                                        │
│                        [Go to Dashboard →]             │
└────────────────────────────────────────────────────────┘
```

### Done Condition for Step 5
```
□ Onboarding layout has no sidebar or navigation
□ Install command displayed with one-click copy button
□ Token expiry countdown works correctly
□ Page updates automatically when server connects (WebSocket)
□ Auto-advance to next step on server connection
□ Template cards show 3 common options
□ Custom image input works with validation
□ Deploy progress shows step-by-step updates from WebSocket
□ Success screen shows the live URL
□ Success screen links to the dashboard
□ Expired token shows regenerate option
```

---

## Step 6 — Zone 2: Day-to-Day Dashboard

### Step 6A — Dashboard Layout

```
The dashboard layout wraps all day-to-day pages.

┌──────────────────────────────────────────────────────────┐
│  YourPlatform    🔔 2          Alice Smith  ▼            │
├──────────────────────────────────────────────────────────┤
│           │                                              │
│  Servers  │         Main content area                    │
│  ──────── │                                              │
│  ● srv-1  │         Changes per page:                   │
│  ○ srv-2  │         - Server overview                   │
│           │         - App list                           │
│  [+ Add   │         - Log viewer                         │
│   Server] │         - Backups                            │
│           │         - Settings                           │
│  ──────── │                                              │
│  Account  │                                              │
│  Support  │                                              │
└──────────────────────────────────────────────────────────┘

Sidebar items:
  → Each server listed with a status dot:
    ● green: connected
    ● yellow: updating
    ○ grey: disconnected
    ● red: error
  
  → Clicking a server navigates to that server's overview
  → Currently selected server is highlighted

Header:
  → Bell icon with unread alert count
  → User name with dropdown: Profile, Settings, Logout
  → Connection status indicator (subtle, not alarming):
    → Small dot next to logo: green if WS connected, yellow if reconnecting
```

### Step 6B — Server Overview Page

```
/dashboard/servers/[server_id]

The first thing user sees when they open a server.
Must communicate health in under 5 seconds.

Layout:
┌────────────────────────────────────────────────────────────┐
│  Production Server          ● Connected    [+ Deploy App]  │
├────────────────────────────────────────────────────────────┤
│                                                            │
│  CPU            RAM              Disk                      │
│  ┌──────────┐   ┌──────────┐     ┌──────────┐             │
│  │  ████    │   │  ██████  │     │  ████    │             │
│  │  45%     │   │  60%     │     │  46%     │             │
│  │          │   │  1.2/2GB │     │  18/40GB │             │
│  └──────────┘   └──────────┘     └──────────┘             │
│                                                            │
├────────────────────────────────────────────────────────────┤
│                                                            │
│  Apps                                          [+ New App] │
│                                                            │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  myshop          ● Running                          │   │
│  │  https://myshop.srv-abc.yourplatform.app   [Open ↗] │   │
│  │  nginx:latest  •  Deployed 2 hours ago              │   │
│  │                    [Logs] [Restart] [Deploy] [···]  │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                            │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  myblog          ✗ Crashed                          │   │
│  │  myblog crashed 5 minutes ago. [View Logs]          │   │
│  │  wordpress:6.4  •  Deployed 3 days ago              │   │
│  │                    [Logs] [Restart] [Deploy] [···]  │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                            │
├────────────────────────────────────────────────────────────┤
│                                                            │
│  ⚠ 1 Alert                                                 │
│  myblog has stopped unexpectedly. [View] [Acknowledge]     │
│                                                            │
│  ✓ Last backup: today at 2:04am (47MB)          [Backups]  │
└────────────────────────────────────────────────────────────┘

Real-time behavior:
  → CPU/RAM/Disk gauges update every 30 seconds (from WS health_report)
  → App status badges update in real time (running → crashed instantly)
  → New alerts appear without page refresh
  → Backup status updates after each backup run
```

### Step 6C — App Detail Page

```
/dashboard/servers/[server_id]/apps/[app_id]

┌────────────────────────────────────────────────────────────┐
│  ← Back to Server        myshop          ● Running        │
├────────────────────────────────────────────────────────────┤
│                                                            │
│  Domain:   https://myshop.srv-abc.yourplatform.app  [↗]   │
│  Image:    nginx:latest                                    │
│  Deployed: 2 hours ago by Alice Smith                      │
│                                                            │
│  [Deploy New Version] [Rollback] [Restart] [Stop] [Delete] │
│                                                            │
├────────────────────────────────────────────────────────────┤
│  Tabs:  [Overview]  [Logs]  [Deployments]  [Settings]      │
├────────────────────────────────────────────────────────────┤
│                                                            │
│  OVERVIEW TAB:                                             │
│                                                            │
│  Container Health                                          │
│  App:       ● Healthy   CPU: 12%   RAM: 187MB / 512MB      │
│  Postgres:  ● Healthy   CPU:  2%   RAM:  98MB / 512MB      │
│                                                            │
│  Environment Variables                                     │
│  DATABASE_URL    ••••••••••••••  [Edit]  (auto-managed)    │
│  STRIPE_KEY      ••••••••••••••  [Edit]                    │
│  NODE_ENV        ••••••••••••••  [Edit]                    │
│                                                  [+ Add]   │
│                                                            │
│  Custom Domains                                            │
│  No custom domains yet.                                    │
│  [+ Add Custom Domain]                                     │
│                                                            │
└────────────────────────────────────────────────────────────┘
```

### Step 6D — Log Viewer Page

```
/dashboard/servers/[server_id]/apps/[app_id]/logs

┌────────────────────────────────────────────────────────────┐
│  ← App Detail    myshop logs                              │
├────────────────────────────────────────────────────────────┤
│                                                            │
│  Container: [App ▼]          ● Live                       │
│                                                            │
├────────────────────────────────────────────────────────────┤
│  ┌──────────────────────────────────────────────────────┐  │
│  │ 10:00:30  GET /api/users 200 45ms                   │  │
│  │ 10:00:31  GET /api/products 200 23ms                │  │
│  │ 10:00:45  POST /api/orders 201 156ms                │  │
│  │ 10:01:02  GET /api/users 200 44ms                   │  │
│  │                                                     │  │
│  │ 10:01:15  ERROR: Cannot connect to redis            │  │◄ stderr (red)
│  │ 10:01:16  Retrying connection...                    │  │
│  │                                                     │  │
│  │ 10:01:20  GET /api/users 200 45ms                   │  │
│  │                                      ▼ (auto-scroll)│  │
│  └──────────────────────────────────────────────────────┘  │
│                                                            │
│  stderr shown in red                                       │
│  Timestamps in local time                                  │
└────────────────────────────────────────────────────────────┘

Behavior:
  → Opens log stream WebSocket on page load
  → Shows last 200 lines immediately
  → New lines appear at the bottom in real time
  → Auto-scrolls to bottom (unless user scrolled up)
  → If user scrolled up: show "↓ New logs" button
  → Clicking that button: scroll to bottom, resume auto-scroll
  → stderr lines displayed in red/orange
  → Timestamps formatted to user's local timezone
  → Selecting different container rerequests logs for that container
```

### Step 6E — Deploy Dialog

```
Triggered by clicking "Deploy New Version" from any app view.

┌────────────────────────────────────────────────────────────┐
│  Deploy myshop                                    ✕        │
├────────────────────────────────────────────────────────────┤
│                                                            │
│  Docker Image                                              │
│  ┌──────────────────────────────────────────────────────┐  │
│  │ nginx:latest                                         │  │
│  └──────────────────────────────────────────────────────┘  │
│                                                            │
│  Current: nginx:1.24.0 (deployed 2 hours ago)             │
│                                                            │
│  Advanced settings ▼                                       │
│                                                            │
│  [Cancel]                           [Deploy →]            │
└────────────────────────────────────────────────────────────┘

After clicking Deploy:
  → Dialog content changes to progress view
  → Same dialog, different content (no modal flash)
  
┌────────────────────────────────────────────────────────────┐
│  Deploying myshop...                              ✕        │
├────────────────────────────────────────────────────────────┤
│                                                            │
│  ✓  Pulling nginx:latest (47MB)                            │
│  ✓  Setting up databases                                   │
│  ✓  Preparing storage                                      │
│  ⏳  Starting your app...                                  │
│  ○  Configuring HTTPS                                      │
│                                                            │
│  ██████████████████░░░░░░░░░░  65%                         │
│                                                            │
│  Estimated time remaining: 15 seconds                      │
│                                                            │
│  [View Logs]                                               │
└────────────────────────────────────────────────────────────┘

On success:
┌────────────────────────────────────────────────────────────┐
│  ✓ myshop deployed successfully                   ✕        │
├────────────────────────────────────────────────────────────┤
│                                                            │
│  nginx:latest is live at:                                  │
│  https://myshop.srv-abc.yourplatform.app          [Open ↗] │
│                                                            │
│  Deploy took 47 seconds.                                   │
│                                                            │
│                                            [Done]          │
└────────────────────────────────────────────────────────────┘

On failure:
┌────────────────────────────────────────────────────────────┐
│  ✗ Deploy failed                                  ✕        │
├────────────────────────────────────────────────────────────┤
│                                                            │
│  Your app did not start correctly.                         │
│  Your previous version is still running.                   │
│                                                            │
│  What went wrong (last log lines from the app):            │
│  ┌──────────────────────────────────────────────────────┐  │
│  │ Error: Cannot connect to database                    │  │
│  │ Check DATABASE_URL environment variable              │  │
│  └──────────────────────────────────────────────────────┘  │
│                                                            │
│  Common causes:                                            │
│  → Missing or incorrect environment variable               │
│  → Database not running                                    │
│  → Port mismatch                                           │
│                                                            │
│  [View Full Logs]                    [Try Again]           │
└────────────────────────────────────────────────────────────┘
```

### Done Condition for Step 6
```
□ Server overview shows metrics, apps, and alerts on one screen
□ Metrics gauges update in real time from WebSocket
□ App status badges update in real time
□ Crashed app shows specific alert with action buttons
□ Deploy dialog shows progress updates from WebSocket
□ Deploy dialog shows specific log lines on failure
□ Log viewer shows last 200 lines on open
□ Log viewer streams new lines with under 100ms visible delay
□ Auto-scroll pauses when user scrolls up
□ Auto-scroll resumes on "↓ New logs" click
□ stderr lines shown in different color
□ Rollback shows available previous deployment
```

---

## Step 7 — Hooks

### Step 7A — useCommand Hook

```typescript
// hooks/use-command.ts

import { useEffect, useRef } from 'react'
import { getWsClient } from '@/lib/ws'
import { useWsStore } from '@/store/ws-store'
import {
  CommandProgressMessage,
  CommandResultMessage
} from '@/types'

interface UseCommandOptions {
  onProgress?: (progress: CommandProgressMessage) => void
  onSuccess?: (result: unknown) => void
  onFailure?: (error: string) => void
}

export function useCommand(
  commandId: string | null,
  options: UseCommandOptions
) {
  const { setCommandProgress, clearCommand } = useWsStore()
  const optionsRef = useRef(options)
  optionsRef.current = options

  useEffect(() => {
    if (!commandId) return

    const ws = getWsClient()

    const unsubProgress = ws.on(
      'command_progress',
      (msg) => {
        const message = msg as unknown as CommandProgressMessage
        if (message.command_id !== commandId) return

        setCommandProgress(commandId, {
          commandId,
          step: message.step,
          message: message.message,
          percent: message.percent,
          stepNumber: message.step_number,
          totalSteps: message.total_steps,
        })
        optionsRef.current.onProgress?.(message)
      }
    )

    const unsubResult = ws.on(
      'command_result',
      (msg) => {
        const message = msg as unknown as CommandResultMessage
        if (message.command_id !== commandId) return

        clearCommand(commandId)

        if (message.status === 'success') {
          optionsRef.current.onSuccess?.(message.result)
        } else {
          optionsRef.current.onFailure?.(
            message.error ?? 'Command failed'
          )
        }
      }
    )

    return () => {
      unsubProgress()
      unsubResult()
    }
  }, [commandId, setCommandProgress, clearCommand])
}
```

### Step 7B — useServerMetrics Hook

```typescript
// hooks/use-metrics.ts

import { useEffect } from 'react'
import { getWsClient } from '@/lib/ws'
import { useServerStore } from '@/store/server-store'
import { ServerUpdateMessage } from '@/types'

export function useServerMetrics(serverId: string | null) {
  const { updateMetrics, updateContainers, addAlert } = useServerStore()

  useEffect(() => {
    if (!serverId) return

    const ws = getWsClient()

    // Subscribe to this server's updates
    ws.subscribe(serverId)

    const unsubUpdate = ws.on('server_update', (msg) => {
      const message = msg as unknown as ServerUpdateMessage
      if (message.server_id !== serverId) return

      updateMetrics(message.metrics)
      updateContainers(message.containers)

      // New alerts from this update
      message.alerts.forEach((alert) => addAlert(alert))
    })

    const unsubConnected = ws.on('agent_connected', () => {
      // Server came back online
    })

    const unsubDisconnected = ws.on('agent_disconnected', () => {
      // Server went offline
    })

    return () => {
      ws.unsubscribe(serverId)
      unsubUpdate()
      unsubConnected()
      unsubDisconnected()
    }
  }, [serverId, updateMetrics, updateContainers, addAlert])

  return useServerStore((state) => ({
    metrics: state.metrics,
    containers: state.containers,
    activeAlerts: state.activeAlerts,
  }))
}
```

### Step 7C — useLogs Hook

```typescript
// hooks/use-logs.ts

import { useEffect, useRef, useCallback } from 'react'
import { useState } from 'react'
import { getWsClient } from '@/lib/ws'
import { LogLine, WsMessage } from '@/types'
import api from '@/lib/api'

interface UseLogsOptions {
  serverId: string
  appId: string
  container?: string
}

export function useLogs({ serverId, appId, container = 'app' }: UseLogsOptions) {
  const [lines, setLines] = useState<LogLine[]>([])
  const [streaming, setStreaming] = useState(false)
  const streamIdRef = useRef<string | null>(null)

  const addLines = useCallback((newLines: LogLine[]) => {
    setLines((prev) => {
      const combined = [...prev, ...newLines]
      // Keep last 2000 lines in memory to avoid DOM overload
      return combined.slice(-2000)
    })
  }, [])

  useEffect(() => {
    const ws = getWsClient()
    const streamId = `stream-${serverId}-${appId}-${container}-${Date.now()}`
    streamIdRef.current = streamId

    // Request log stream
    ws.send({
      type: 'start_log_stream',
      server_id: serverId,
      app_id: appId,
      container,
      stream_id: streamId,
    })
    setStreaming(true)

    // Receive initial batch of historical logs
    const unsubInitial = ws.on('initial_logs', (msg: WsMessage) => {
      const message = msg as {
        stream_id: string
        lines: LogLine[]
      } & WsMessage
      if (message.stream_id !== streamId) return
      setLines(message.lines)
    })

    // Receive live log lines
    const unsubLines = ws.on('log_lines', (msg: WsMessage) => {
      const message = msg as {
        stream_id: string
        lines: LogLine[]
      } & WsMessage
      if (message.stream_id !== streamId) return
      addLines(message.lines)
    })

    // Stream ended (container stopped)
    const unsubEnded = ws.on('stream_ended', (msg: WsMessage) => {
      const message = msg as {
        stream_id: string
        reason: string
      } & WsMessage
      if (message.stream_id !== streamId) return
      setStreaming(false)
      addLines([{
        timestamp: new Date().toISOString(),
        stream: 'stdout',
        text: `--- Stream ended: ${message.reason} ---`,
      }])
    })

    return () => {
      // Stop the stream
      if (streamIdRef.current) {
        ws.send({
          type: 'stop_log_stream',
          stream_id: streamIdRef.current,
        })
      }
      unsubInitial()
      unsubLines()
      unsubEnded()
      setStreaming(false)
    }
  }, [serverId, appId, container, addLines])

  return { lines, streaming }
}
```

### Done Condition for Step 7
```
□ useCommand tracks progress and result for a given command ID
□ useCommand calls correct callbacks on success and failure
□ useServerMetrics subscribes to server on mount, unsubscribes on unmount
□ useServerMetrics updates stores with incoming data
□ useLogs starts a stream on mount, stops it on unmount
□ useLogs keeps last 2000 lines to prevent memory issues
□ useLogs handles stream_ended event gracefully
□ All hooks clean up their WebSocket listeners on unmount
```

---

## Step 8 — Error States and Loading States

### Error States

```
Every data-fetching page must handle:

1. Loading state
   → Show skeleton UI (not spinner)
   → Skeleton matches the shape of the actual content
   → User understands what is loading

2. Error state
   → Show what went wrong in plain English
   → Offer an action (Retry, Go back, Contact support)
   → Never show a raw error message or stack trace
   → Never show a blank white page

3. Empty state
   → No servers yet: show "Add your first server" prompt
   → No apps yet: show "Deploy your first app" prompt
   → No alerts: show "All clear" message (this is good news)
   → No backups: explain that the first backup runs tonight

Error component:
┌────────────────────────────────────────────────────────┐
│                                                        │
│              Something went wrong                      │
│                                                        │
│  We could not load your server information.           │
│  This is usually a temporary issue.                   │
│                                                        │
│  [Try again]          [Contact support]               │
│                                                        │
│  (request ID: req-abc123 — include this if            │
│   you contact support)                                │
└────────────────────────────────────────────────────────┘

Server offline state (shown in server overview):
┌────────────────────────────────────────────────────────┐
│  Production Server          ○ Disconnected             │
├────────────────────────────────────────────────────────┤
│                                                        │
│  Your server is not currently connected.               │
│  Your apps are still running.                          │
│                                                        │
│  This usually means:                                   │
│  → The server was rebooted (reconnects in < 1 min)    │
│  → A network interruption (reconnects automatically)  │
│                                                        │
│  If disconnected for more than 5 minutes:             │
│  → SSH into your server and check:                    │
│    sudo systemctl status yourplatform-agent           │
│                                                        │
│  Last seen: 3 minutes ago                             │
└────────────────────────────────────────────────────────┘
```

### Loading States (Skeleton UI)

```
Skeleton for server overview:

┌────────────────────────────────────────────────────────┐
│  ████████████████           ████████     ████████████  │
├────────────────────────────────────────────────────────┤
│                                                        │
│  ┌──────────┐   ┌──────────┐   ┌──────────┐           │
│  │░░░░░░░░░░│   │░░░░░░░░░░│   │░░░░░░░░░░│           │
│  │░░░░░░░░░░│   │░░░░░░░░░░│   │░░░░░░░░░░│           │
│  └──────────┘   └──────────┘   └──────────┘           │
│                                                        │
│  ┌──────────────────────────────────────────────────┐  │
│  │░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░│  │
│  └──────────────────────────────────────────────────┘  │
│  ┌──────────────────────────────────────────────────┐  │
│  │░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░│  │
│  └──────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────┘

The skeleton has the same layout as the real content.
User knows exactly what is coming.
Smooth transition from skeleton to content (no layout shift).
```

### Done Condition for Step 8
```
□ Every page has a loading skeleton matching actual content shape
□ Every page has an error state with plain-English message
□ Error state includes request ID for support
□ Error state has a Retry action
□ Empty states have helpful prompts (not blank screens)
□ Server offline state explains the situation calmly
□ No raw error objects or stack traces ever shown to user
□ No blank white pages under any error condition
```

---

## Layer 7 Overall Done Condition

```
The full user experience test:

Test 1 — New user onboarding:
  □ Register with email + password
  □ Directed to onboarding flow
  □ See the install command immediately
  □ Paste command on a fresh Ubuntu server
  □ Dashboard updates to "Connected" automatically
  □ Directed to deploy step
  □ Select Next.js template
  □ Watch deploy progress in real time
  □ See live URL on success
  □ Click URL: app is live with HTTPS

Test 2 — Day-to-day use:
  □ Log in
  □ See server overview immediately
  □ Metrics gauges show real data
  □ All apps show correct status
  □ Deploy an update: progress visible without page refresh
  □ Open logs: see history + live stream
  □ Close logs: stream stops

Test 3 — Incident response:
  □ App crashes
  □ Status badge changes to "Crashed" in real time
  □ Alert appears in dashboard in real time
  □ Alert shows what went wrong in plain English
  □ Alert shows what to do
  □ One-click restart from alert or app card
  □ Status badge returns to "Running"
  □ "Resolved" alert appears

Test 4 — Team member experience:
  □ Owner invites a member
  □ Member logs in
  □ Member sees the server
  □ Member can deploy (202 response)
  □ Member cannot delete server (403, clear message)

Test 5 — Mobile browser (basic):
  □ Dashboard is readable on a phone
  □ Key info visible without horizontal scroll
  □ Deploy button accessible
  □ Log viewer readable (horizontal scroll acceptable here)

Test 6 — Offline browser:
  □ Browser loses internet
  □ WebSocket shows reconnecting indicator
  □ Browser comes back online
  □ WebSocket reconnects automatically
  □ Dashboard shows current state without page refresh

Test 7 — Slow connection:
  □ All pages show skeleton while loading
  □ No blank white pages
  □ No layout shifts when content loads

When all 7 tests pass, Layer 7 is done.
The MVP is complete.
```

---

## What Layer 7 Does NOT Do

```
Layer 7 does not:
├── Store any server-side state (stateless Next.js pages)
├── Talk to agents directly (goes through Layer 6 API)
├── Handle authentication on the server side
│   (tokens are in localStorage, validated client-side for routing)
├── Process WebSocket messages from agents
│   (Layer 5B processes them, forwards results to browser WS)
├── Perform any Docker or Caddy operations
│   (all through the command system: Layer 6 → Layer 5B → Agent)
└── Show technical details to non-technical users
    (every visible message is translated to plain English)

Layer 7 is the face of the product.
Every word, every color, every button placement
is a product decision.
The technology underneath exists to make this layer work.
```

---

## MVP Complete

```
All 7 layers are planned and defined:

Layer 1  ✓  Install layer
Layer 2  ✓  Server environment detection
Layer 3A ✓  Docker management
Layer 3B ✓  Caddy management
Layer 3C ✓  Restic backups
Layer 4A ✓  Agent lifecycle
Layer 4B ✓  Command executor
Layer 4C ✓  Health and log reporter
Layer 5A ✓  Auth and sessions
Layer 5B ✓  WebSocket hub
Layer 5C ✓  Database (SQLite)
Layer 6  ✓  Control plane API
Layer 7  ✓  Frontend dashboard

Build order:
  Layer 2 → Layer 3A → Layer 3B → Layer 4A → Layer 4B → Layer 4C
  (agent is functional end-to-end before touching the control plane)

  Layer 5C → Layer 5A → Layer 5B → Layer 6 → Layer 7
  (control plane built from bottom up)

  Layer 1 connects them: install script ties agent to control plane.
```