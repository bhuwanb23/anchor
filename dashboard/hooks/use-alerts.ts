"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { WSClient, WSMessage } from "@/lib/ws";
import { Alert } from "@/types";
import api from "@/lib/api";

export interface AlertItem extends Alert {
  at: string;
}

const MAX_ALERTS = 100;

/**
 * Subscribes to a server's browser WebSocket for live alerts (anomaly_alert
 * plus Caddy error_alert) and loads the persisted alert history from the
 * control plane on mount. Active alerts float to the top.
 */
export function useAlerts(serverId: string, enabled = true) {
  const [alerts, setAlerts] = useState<AlertItem[]>([]);
  const clientRef = useRef<WSClient | null>(null);
  const loadedRef = useRef(false);

  // Load persisted alert history from the control plane.
  useEffect(() => {
    if (!enabled || !serverId || loadedRef.current) return;
    loadedRef.current = true;
    api
      .get<Alert[]>(`/servers/${serverId}/alerts`)
      .then((res) => {
        const items: AlertItem[] = (res.data || []).map((a) => ({
          ...a,
          at: a.fired_at || a.resolved_at || new Date().toISOString(),
        }));
        setAlerts(items.slice(0, MAX_ALERTS));
      })
      .catch(() => {
        // Live feed still works even if history fetch fails.
      });
  }, [enabled, serverId]);

  /** Marks an active alert as acknowledged (Layer 4C Step 6). */
  const acknowledge = useCallback(
    async (id: string) => {
      try {
        await api.post(`/servers/${serverId}/alerts/${id}/ack`);
        const now = new Date().toISOString();
        setAlerts((prev) =>
          prev.map((a) =>
            a.id === id && a.status === "active"
              ? { ...a, status: "acknowledged", acknowledged_at: now }
              : a
          )
        );
      } catch {
        // Ignore: alert list will reconcile on next fetch.
      }
    },
    [serverId]
  );

  const handleMessage = useCallback((msg: WSMessage) => {
    if (msg.type !== "anomaly_alert" && msg.type !== "error_alert") return;
    const a = msg.payload as Partial<Alert>;
    if (!a?.message && !a?.title) return;

    const level =
      a.status === "resolved" || a.level === "resolved"
        ? "resolved"
        : a.severity === "critical" || a.level === "critical"
        ? "critical"
        : "warning";

    const item: AlertItem = {
      id: a.id ?? `live-${Date.now()}`,
      server_id: a.server_id ?? serverId,
      project: a.project,
      container: a.container,
      level,
      severity: (a.severity === "critical" ? "critical" : "warning") as "critical" | "warning",
      type: a.type ?? "alert",
      status: (level === "resolved" ? "resolved" : "active") as AlertItem["status"],
      title: a.title || a.message || "Alert",
      message: a.message || a.title || "",
      detail: a.detail,
      action: a.action,
      fired_at: a.fired_at || new Date().toISOString(),
      resolved_at: a.resolved_at ?? null,
      read_at: null,
      acknowledged_at: null,
      at: new Date().toISOString(),
    };

    setAlerts((prev) => {
      // Replace in place if the id matches (escalation/resolution updates),
      // otherwise prepend.
      const without = prev.filter((x) => x.id !== item.id);
      return [item, ...without].slice(0, MAX_ALERTS);
    });
  }, [serverId]);

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

  return { alerts, acknowledge };
}
