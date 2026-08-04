"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { WSClient, WSMessage } from "@/lib/ws";
import { AnomalyAlert } from "@/types";

export interface AlertItem extends AnomalyAlert {
  at: string;
}

const MAX_ALERTS = 50;

/**
 * Subscribes to a server's browser WebSocket and collects live anomaly alerts
 * (anomaly_alert) plus Caddy error alerts (error_alert) for display.
 */
export function useAlerts(serverId: string, enabled = true) {
  const [alerts, setAlerts] = useState<AlertItem[]>([]);
  const clientRef = useRef<WSClient | null>(null);

  const handleMessage = useCallback((msg: WSMessage) => {
    if (msg.type !== "anomaly_alert" && msg.type !== "error_alert") return;
    const a = msg.payload as Partial<AnomalyAlert>;
    if (!a?.message) return;

    const item: AlertItem = {
      level: a.level === "critical" ? "critical" : a.level === "resolved" ? "resolved" : "warning",
      type: a.type ?? "alert",
      project: a.project,
      container: a.container,
      message: a.message,
      at: new Date().toISOString(),
    };
    setAlerts((prev) => [item, ...prev].slice(0, MAX_ALERTS));
  }, []);

  useEffect(() => {
    if (!enabled || !serverId) return;

    const client = new WSClient(serverId);
    clientRef.current = client;
    const unsub = client.onMessage(handleMessage);
    client.connect();

    return () => {
      unsub();
      client.disconnect();
      clientRef.current = null;
    };
  }, [enabled, serverId, handleMessage]);

  return { alerts };
}
