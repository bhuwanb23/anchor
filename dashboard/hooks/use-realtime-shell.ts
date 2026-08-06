"use client";

import { useEffect } from "react";
import { getWSClient, type WSMessage } from "@/lib/ws";
import { useServerStore } from "@/store/server-store";
import type { Alert, ContainerState, MetricsSnapshot, ServerStatus } from "@/types";

/**
 * Wires global WebSocket handlers for the Zone 2 shell:
 * - agent connect/disconnect → sidebar status dots
 * - health_report / server_state → metrics + containers for selected server
 * - alerts → unread count bump + store
 */
export function useRealtimeShell(selectedServerId: string | null) {
  const updateServerStatus = useServerStore((s) => s.updateServerStatus);
  const updateMetrics = useServerStore((s) => s.updateMetrics);
  const updateContainers = useServerStore((s) => s.updateContainers);
  const addAlert = useServerStore((s) => s.addAlert);
  const setAlerts = useServerStore((s) => s.setAlerts);
  const fetchServers = useServerStore((s) => s.fetchServers);

  // Subscribe to the selected server (snapshot + live health)
  useEffect(() => {
    if (!selectedServerId) return;
    const client = getWSClient();
    client.subscribeServer(selectedServerId);
    return () => {
      client.unsubscribeServer();
    };
  }, [selectedServerId]);

  useEffect(() => {
    const client = getWSClient();

    const applyHealth = (msg: WSMessage) => {
      const sid =
        msg.server_id ||
        (msg.payload as { server_id?: string } | undefined)?.server_id;
      // Flat health_report OR nested under payload
      const flat = msg as unknown as Record<string, unknown>;
      const raw = flat.server
        ? flat
        : ((msg.payload as Record<string, unknown>) || {});
      const serverBlock = (raw.server || {}) as Record<string, number>;
      const containers = (raw.containers || []) as ContainerState[];
      const platform = (raw.platform || {}) as Record<string, unknown>;

      if (sid) {
        updateServerStatus(sid, "connected");
      }
      if (
        selectedServerId &&
        (!sid || sid === selectedServerId) &&
        (serverBlock.cpu_percent !== undefined || serverBlock.ram_percent !== undefined)
      ) {
        const metrics: MetricsSnapshot = {
          cpu_percent: Number(serverBlock.cpu_percent || 0),
          ram_used_mb: Number(serverBlock.ram_used_mb || 0),
          ram_total_mb: Number(serverBlock.ram_total_mb || 0),
          ram_percent: Number(serverBlock.ram_percent || 0),
          disk_used_gb: Number(serverBlock.disk_used_gb || 0),
          disk_total_gb: Number(serverBlock.disk_total_gb || 0),
          disk_percent: Number(serverBlock.disk_percent || 0),
          load_1min: Number(serverBlock.load_1min || 0),
        };
        updateMetrics(metrics);
        if (Array.isArray(containers)) {
          updateContainers(containers);
        }
        if (typeof platform.agent_version === "string") {
          useServerStore.getState().setAgentVersion(selectedServerId, platform.agent_version);
        }
      }
    };

    const unsubs = [
      client.on("agent_connected", (msg) => {
        const sid =
          msg.server_id ||
          (msg.payload as { server_id?: string } | undefined)?.server_id;
        if (sid) updateServerStatus(sid, "connected");
        else fetchServers();
      }),
      client.on("agent_disconnected", (msg) => {
        const sid =
          msg.server_id ||
          (msg.payload as { server_id?: string } | undefined)?.server_id;
        if (sid) updateServerStatus(sid, "disconnected");
        else fetchServers();
      }),
      client.on("health_report", applyHealth),
      client.on("server_update", (msg) => {
        const p = (msg.payload || {}) as {
          status?: ServerStatus;
          metrics?: MetricsSnapshot;
          containers?: ContainerState[];
        };
        const sid = msg.server_id;
        if (sid && p.status) updateServerStatus(sid, p.status);
        if (selectedServerId && (!sid || sid === selectedServerId)) {
          if (p.metrics) updateMetrics(p.metrics);
          if (p.containers) updateContainers(p.containers);
        }
      }),
      client.on("server_state", (msg) => {
        const p = (msg.payload || {}) as {
          server?: {
            id?: string;
            status?: ServerStatus;
            metrics?: MetricsSnapshot;
            containers?: ContainerState[];
            alerts?: Alert[];
          };
        };
        const srv = p.server;
        if (!srv) return;
        if (srv.id && srv.status) updateServerStatus(srv.id, srv.status);
        if (selectedServerId && (!srv.id || srv.id === selectedServerId)) {
          if (srv.metrics) updateMetrics(srv.metrics);
          if (srv.containers) updateContainers(srv.containers);
          if (srv.alerts) setAlerts(srv.alerts);
        }
      }),
      client.on("anomaly_alert", (msg) => {
        const p = (msg.payload || {}) as Partial<Alert>;
        if (p.id) {
          addAlert({
            id: p.id,
            server_id: p.server_id || msg.server_id || selectedServerId || "",
            severity: p.severity || "warning",
            type: p.type || "anomaly",
            status: p.status || "active",
            title: p.title,
            message: p.message || "",
            project: p.project,
            fired_at: p.fired_at || new Date().toISOString(),
          });
        }
      }),
      client.on("error_alert", (msg) => {
        const p = (msg.payload || {}) as Partial<Alert>;
        if (p.id) {
          addAlert({
            id: p.id,
            server_id: p.server_id || msg.server_id || selectedServerId || "",
            severity: p.severity || "critical",
            type: p.type || "error",
            status: p.status || "active",
            title: p.title,
            message: p.message || "",
            project: p.project,
            fired_at: p.fired_at || new Date().toISOString(),
          });
        }
      }),
    ];

    return () => unsubs.forEach((u) => u());
  }, [
    selectedServerId,
    updateServerStatus,
    updateMetrics,
    updateContainers,
    addAlert,
    setAlerts,
    fetchServers,
  ]);
}
