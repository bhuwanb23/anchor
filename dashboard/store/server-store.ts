"use client";

import { create } from "zustand";
import api from "@/lib/api";
import type {
  Server,
  ServerStatus,
  MetricsSnapshot,
  ContainerState,
  Alert,
  AlertStatus,
} from "@/types";

interface ServerState {
  servers: Server[];
  loading: boolean;
  fetchServers: () => Promise<void>;
  updateServerStatus: (id: string, status: ServerStatus) => void;
  setAgentVersion: (id: string, version: string) => void;

  selectedServerId: string | null;
  selectServer: (id: string | null) => void;

  metrics: MetricsSnapshot | null;
  updateMetrics: (metrics: MetricsSnapshot) => void;

  containers: ContainerState[];
  updateContainers: (containers: ContainerState[]) => void;

  alerts: Alert[];
  setAlerts: (alerts: Alert[]) => void;
  addAlert: (alert: Alert) => void;
  resolveAlert: (id: string) => void;
  acknowledgeAlert: (id: string) => void;

  unreadCount: number;
  setUnreadCount: (n: number) => void;
  bumpUnread: () => void;

  getServer: (id: string) => Server | undefined;
}

export const useServerStore = create<ServerState>((set, get) => ({
  servers: [],
  loading: true,

  fetchServers: async () => {
    try {
      const res = await api.get<Server[]>("/api/v1/servers");
      const list = (res.data || []).map((s) => ({
        ...s,
        // Normalize CP field names
        public_ip: s.public_ip || (s as { ip_address?: string }).ip_address,
        os: s.os || (s as { os_info?: string }).os_info,
        ram_total_mb: s.ram_total_mb || (s as { ram_mb?: number }).ram_mb,
        disk_total_gb: s.disk_total_gb || (s as { disk_gb?: number }).disk_gb,
      }));
      set({ servers: list, loading: false });
    } catch {
      set({ loading: false });
    }
  },

  updateServerStatus: (id, status) => {
    set((state) => ({
      servers: state.servers.map((s) => (s.id === id ? { ...s, status } : s)),
    }));
  },

  setAgentVersion: (id, version) => {
    set((state) => ({
      servers: state.servers.map((s) =>
        s.id === id ? { ...s, agent_version: version } : s
      ),
    }));
  },

  selectedServerId: null,

  selectServer: (id) => {
    const prev = get().selectedServerId;
    if (prev === id) {
      set({ selectedServerId: id });
      return;
    }
    set({
      selectedServerId: id,
      metrics: null,
      containers: [],
      alerts: [],
    });
  },

  metrics: null,
  updateMetrics: (metrics) => set({ metrics }),

  containers: [],
  updateContainers: (containers) => set({ containers }),

  alerts: [],
  setAlerts: (alerts) => set({ alerts }),
  addAlert: (alert) => {
    set((state) => {
      const without = state.alerts.filter((a) => a.id !== alert.id);
      return { alerts: [alert, ...without] };
    });
  },
  resolveAlert: (id) => {
    set((state) => ({
      alerts: state.alerts.map((a) =>
        a.id === id
          ? { ...a, status: "resolved" as AlertStatus, resolved_at: new Date().toISOString() }
          : a
      ),
    }));
  },
  acknowledgeAlert: (id) => {
    set((state) => ({
      alerts: state.alerts.map((a) =>
        a.id === id
          ? { ...a, status: "acknowledged" as AlertStatus, acknowledged_at: new Date().toISOString() }
          : a
      ),
    }));
  },

  unreadCount: 0,
  setUnreadCount: (n) => set({ unreadCount: n }),
  bumpUnread: () => set((s) => ({ unreadCount: s.unreadCount + 1 })),

  getServer: (id) => get().servers.find((s) => s.id === id),
}));
