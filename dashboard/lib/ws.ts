import { getAccessToken } from "./api";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface WSMessage {
  type: string;
  server_id?: string;
  payload?: unknown;
}

export type MessageHandler = (msg: WSMessage) => void;
type ConnectionHandler = () => void;

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const WS_URL = process.env.NEXT_PUBLIC_WS_URL || "ws://localhost:8080/ws/browser";
const PING_INTERVAL_MS = 30_000;
const PONG_TIMEOUT_MS = 10_000;
const MAX_BACKOFF_SEC = 60;

// ---------------------------------------------------------------------------
// BrowserWSClient — singleton WebSocket client for the dashboard
//
// - One connection shared by the entire app
// - JWT in query parameter (browsers cannot set WS headers)
// - Automatic reconnection with exponential backoff + jitter
// - Heartbeat ping/pong to detect dead connections
// - Typed message routing to registered handlers
// - Server subscription management
// ---------------------------------------------------------------------------

class BrowserWSClient {
  private ws: WebSocket | null = null;
  private handlers = new Map<string, Set<MessageHandler>>();
  private connectHandlers = new Set<ConnectionHandler>();
  private disconnectHandlers = new Set<ConnectionHandler>();
  private subscribedServerId: string | null = null;

  // Reconnection state
  private reconnectAttempt = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private intentionalClose = false;
  private autoReconnectPending = false;
  private reconnectingHandlers = new Set<ConnectionHandler>();

  // Heartbeat state
  private pingTimer: ReturnType<typeof setInterval> | null = null;
  private pongTimer: ReturnType<typeof setTimeout> | null = null;
  private alive = false;

  // ---------------------------------------------------------------------------
  // Connection lifecycle
  // ---------------------------------------------------------------------------

  connect(): void {
    if (this.ws?.readyState === WebSocket.OPEN || this.ws?.readyState === WebSocket.CONNECTING) {
      return;
    }

    const token = getAccessToken();
    if (!token) return;

    this.intentionalClose = false;
    const url = `${WS_URL}?token=${encodeURIComponent(token)}`;
    this.ws = new WebSocket(url);

    this.ws.onopen = () => {
      console.log("[ws] connected");
      this.reconnectAttempt = 0;
      this.autoReconnectPending = false;
      this.alive = true;
      this.startPing();
      this.connectHandlers.forEach((h) => h());

      // Re-subscribe if we had a server subscription
      if (this.subscribedServerId) {
        this.send({ type: "subscribe", server_id: this.subscribedServerId });
      }
    };

    this.ws.onmessage = (event) => {
      try {
        const msg: WSMessage = JSON.parse(event.data);
        this.routeMessage(msg);
      } catch (e) {
        console.error("[ws] parse error", e);
      }
    };

    this.ws.onclose = () => {
      console.log("[ws] disconnected");
      this.stopPing();
      this.alive = false;
      this.disconnectHandlers.forEach((h) => h());
      if (!this.intentionalClose) {
        this.autoReconnectPending = true;
        this.reconnectingHandlers.forEach((h) => h());
        this.scheduleReconnect();
      } else {
        this.autoReconnectPending = false;
      }
    };

    this.ws.onerror = (err) => {
      console.error("[ws] error", err);
      // onclose will fire after onerror, triggering reconnect
    };
  }

  disconnect(): void {
    this.intentionalClose = true;
    this.autoReconnectPending = false;
    this.stopPing();
    this.clearReconnect();
    this.ws?.close();
    this.ws = null;
  }

  /** True while exponential backoff reconnect is scheduled or in flight. */
  isReconnecting(): boolean {
    return this.autoReconnectPending && !this.intentionalClose;
  }

  // ---------------------------------------------------------------------------
  // Message sending
  // ---------------------------------------------------------------------------

  send(msg: WSMessage): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(msg));
    }
  }

  // ---------------------------------------------------------------------------
  // Handler registration — components register per message type
  // Returns an unsubscribe function for clean unmount
  // ---------------------------------------------------------------------------

  on(type: string, handler: MessageHandler): () => void {
    if (!this.handlers.has(type)) {
      this.handlers.set(type, new Set());
    }
    this.handlers.get(type)!.add(handler);
    return () => {
      this.handlers.get(type)?.delete(handler);
    };
  }

  /** Register a handler that fires on every message (regardless of type). */
  onAny(handler: MessageHandler): () => void {
    return this.on("*", handler);
  }

  onConnect(handler: ConnectionHandler): () => void {
    this.connectHandlers.add(handler);
    return () => {
      this.connectHandlers.delete(handler);
    };
  }

  onDisconnect(handler: ConnectionHandler): () => void {
    this.disconnectHandlers.add(handler);
    return () => {
      this.disconnectHandlers.delete(handler);
    };
  }

  onReconnecting(handler: ConnectionHandler): () => void {
    this.reconnectingHandlers.add(handler);
    return () => {
      this.reconnectingHandlers.delete(handler);
    };
  }

  // ---------------------------------------------------------------------------
  // Server subscription — hub only sends updates for the subscribed server
  // ---------------------------------------------------------------------------

  subscribeServer(serverId: string): void {
    this.subscribedServerId = serverId;
    this.send({ type: "subscribe", server_id: serverId });
  }

  unsubscribeServer(): void {
    if (this.subscribedServerId) {
      this.send({ type: "unsubscribe", server_id: this.subscribedServerId });
    }
    this.subscribedServerId = null;
  }

  // ---------------------------------------------------------------------------
  // Internals — message routing, heartbeat, reconnection
  // ---------------------------------------------------------------------------

  private routeMessage(msg: WSMessage): void {
    // Fire type-specific handlers
    const typeHandlers = this.handlers.get(msg.type);
    if (typeHandlers) {
      typeHandlers.forEach((h) => h(msg));
    }

    // Fire wildcard handlers (registered via onAny)
    const wildcardHandlers = this.handlers.get("*");
    if (wildcardHandlers) {
      wildcardHandlers.forEach((h) => h(msg));
    }
  }

  // -- Heartbeat -------------------------------------------------------------

  private startPing(): void {
    this.stopPing();
    this.pingTimer = setInterval(() => {
      if (!this.alive) {
        // Previous pong never arrived — connection is dead
        console.warn("[ws] no pong received, closing dead connection");
        this.ws?.close();
        return;
      }
      this.alive = false;
      this.send({ type: "ping" });
      this.pongTimer = setTimeout(() => {
        // Pong timeout — will be caught on next ping cycle
      }, PONG_TIMEOUT_MS);
    }, PING_INTERVAL_MS);
  }

  private stopPing(): void {
    if (this.pingTimer) {
      clearInterval(this.pingTimer);
      this.pingTimer = null;
    }
    if (this.pongTimer) {
      clearTimeout(this.pongTimer);
      this.pongTimer = null;
    }
  }

  // -- Reconnection with exponential backoff + jitter -----------------------

  private scheduleReconnect(): void {
    this.reconnectAttempt++;
    const base = Math.min(MAX_BACKOFF_SEC, Math.pow(2, this.reconnectAttempt - 1));
    // Random jitter: ±25% of the base delay
    const jitter = base * 0.25 * (Math.random() * 2 - 1);
    const delay = Math.max(0.5, base + jitter);
    console.log(`[ws] reconnecting in ${delay.toFixed(1)}s (attempt ${this.reconnectAttempt})`);
    this.reconnectTimer = setTimeout(() => {
      this.connect();
    }, delay * 1000);
  }

  private clearReconnect(): void {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
  }
}

// ---------------------------------------------------------------------------
// Singleton export — one instance for the entire dashboard
// ---------------------------------------------------------------------------

let instance: BrowserWSClient | null = null;

export function getWSClient(): BrowserWSClient {
  if (!instance) {
    instance = new BrowserWSClient();
  }
  return instance;
}

export { BrowserWSClient as WSClient };
export type { BrowserWSClient };
