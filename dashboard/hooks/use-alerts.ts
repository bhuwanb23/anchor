"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { getWSClient, type WSMessage } from "@/lib/ws";
import { Alert } from "@/types";
import api from "@/lib/api";
import { useServerStore } from "@/store/server-store";
import { toast } from "sonner";

export interface AlertItem extends Alert {
  at: string;
}

const MAX_ALERTS = 100;

/**
 * Subscribes to the singleton WebSocket for live alerts (anomaly_alert
 * plus error_alert) and loads the persisted alert history from the
 * control plane on mount. Active alerts float to the top.
 */
export function useAlerts(serverId: string, enabled = true) {
  const [alerts, setAlerts] = useState<AlertItem[]>([]);
  // Use useRef so unsubscribe functions persist across renders
  const unsubRef = useRef<(() => void)[]>([]);

  // Load persisted alert history from the control plane.
  useEffect(() => {
    if (!enabled || !serverId) return;
    api
      .get<Alert[]>(`/api/v1/servers/${serverId}/alerts`)
      .then((res) => {
        const raw = Array.isArray(res.data) ? res.data : [];
        const items: AlertItem[] = raw.map((a) => ({
          ...a,
          at: a.fired_at || a.resolved_at || new Date().toISOString(),
        }));
        setAlerts(items.slice(0, MAX_ALERTS));
        useServerStore.getState().setAlerts(items.slice(0, MAX_ALERTS));
      })
      .catch(() => {
        // Live feed still works even if history fetch fails.
      });
  }, [enabled, serverId]);

  /** Marks an active alert as acknowledged. */
  const acknowledge = useCallback(
    async (id: string) => {
      try {
        await api.post(`/api/v1/servers/${serverId}/alerts/${id}/ack`);
        const now = new Date().toISOString();
        setAlerts((prev) => {
          const next = prev.map((a) =>
            a.id === id && a.status === "active"
              ? { ...a, status: "acknowledged" as const, acknowledged_at: now }
              : a
          );
          useServerStore.getState().acknowledgeAlert(id);
          return next;
        });
        toast.success("Alert acknowledged");
      } catch {
        // Ignore: alert list will reconcile on next fetch.
      }
    },
    [serverId]
  );

  function handleAlert(msg: WSMessage) {
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
      const without = prev.filter((x) => x.id !== item.id);
      return [item, ...without].slice(0, MAX_ALERTS);
    });
    // Keep global store in sync for overview app cards
    useServerStore.getState().addAlert(item);
  }

  // Register WebSocket handler on mount, deregister on unmount.
  useEffect(() => {
    if (!enabled || !serverId) return;

    // Clean up any previous subscriptions
    unsubRef.current.forEach((fn) => fn());
    unsubRef.current = [];

    const client = getWSClient();

    // Subscribe to this server's updates
    client.subscribeServer(serverId);
    unsubRef.current.push(() => client.unsubscribeServer());

    const unsub = client.on("anomaly_alert", handleAlert);
    unsubRef.current.push(unsub);

    const unsub2 = client.on("error_alert", handleAlert);
    unsubRef.current.push(unsub2);

    // Ensure the singleton is connected
    client.connect();

    return () => {
      unsubRef.current.forEach((fn) => fn());
      unsubRef.current = [];
    };
  }, [enabled, serverId]);

  return { alerts, acknowledge };
}
