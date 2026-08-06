import { getToken } from "./auth";

export interface WSMessage {
  type: string;
  server_id?: string;
  payload?: unknown;
}

type MessageHandler = (msg: WSMessage) => void;

export class WSClient {
  private ws: WebSocket | null = null;
  private url: string;
  private handlers: MessageHandler[] = [];
  private baseReconnectSec: number;
  private reconnectAttempt: number = 0;
  private maxReconnectSec: number = 30;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private serverId: string;
  private onConnectCallback: (() => void) | null = null;

  constructor(
    serverId: string,
    url: string = process.env.NEXT_PUBLIC_WS_URL || "ws://localhost:8080/ws/browser",
    baseReconnectSec: number = 1
  ) {
    this.serverId = serverId;
    this.url = url;
    this.baseReconnectSec = baseReconnectSec;
  }

  connect(): void {
    const token = getToken();
    if (!token) return;

    // Connect with JWT token and server_id as query parameters
    const wsUrl = `${this.url}?token=${encodeURIComponent(token)}&server_id=${encodeURIComponent(this.serverId)}`;
    this.ws = new WebSocket(wsUrl);

    this.ws.onopen = () => {
      console.log("WebSocket connected to server", this.serverId);
      this.reconnectAttempt = 0;
      this.onConnectCallback?.();
    };

    this.ws.onmessage = (event) => {
      try {
        const msg: WSMessage = JSON.parse(event.data);
        this.handlers.forEach((h) => h(msg));
      } catch (e) {
        console.error("Failed to parse WS message", e);
      }
    };

    this.ws.onclose = () => {
      console.log("WebSocket disconnected, reconnecting...");
      this.scheduleReconnect();
    };

    this.ws.onerror = (err) => {
      console.error("WebSocket error", err);
    };
  }

  onMessage(handler: MessageHandler): () => void {
    this.handlers.push(handler);
    return () => {
      this.handlers = this.handlers.filter((h) => h !== handler);
    };
  }

  /**
   * Registers a callback fired every time the WebSocket (re)opens.
   * Useful for re-sending stream commands after a browser reconnect.
   */
  onConnect(cb: () => void): () => void {
    this.onConnectCallback = cb;
    return () => {
      if (this.onConnectCallback === cb) this.onConnectCallback = null;
    };
  }

  send(msg: WSMessage): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(msg));
    }
  }

  disconnect(): void {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.ws?.close();
    this.ws = null;
  }

  private scheduleReconnect(): void {
    this.reconnectAttempt++;
    const delay = Math.min(
      this.baseReconnectSec * Math.pow(2, this.reconnectAttempt - 1),
      this.maxReconnectSec
    );
    console.log(`Reconnecting in ${delay}s (attempt ${this.reconnectAttempt})`);
    this.reconnectTimer = setTimeout(() => {
      this.connect();
    }, delay * 1000);
  }
}
