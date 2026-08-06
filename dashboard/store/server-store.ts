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
  AlertSeverity,
} from "@/types";

// ---------------------------------------------------------------------------
// Server Store
//
// Holds the server list, the currently-selected server's real-time data,
// and active alerts. Clearing stale data when the selected server changes
// is handled by selectServer().
// ---------------------------------------------------------------------------

interface ServerState {
  // Server list
  servers: Server[];
  loading: boolean;
  fetchServers: () => Promise<void>;

  // Selected server
  selectedServerId: string | null;
  selectServer: (id: string | null) => void;

  // Real-time metrics (for the selected server, updated by WebSocket)
  metrics: MetricsSnapshot | null;
  updateMetrics: (metrics: MetricsSnapshot) => void;

  // Container states (for the selected server, updated by WebSocket)
  containers: ContainerState[];
  updateContainers: (containers: ContainerState[]) => void;

  // Active alerts (for the selected server)
  alerts: Alert[];
  addAlert: (alert: Alert) => void;
  resolveAlert: (id: string) => void;
  acknowledgeAlert: (id: string) => void;

  // Helpers
  getServer: (id: string) => Server | undefined;
}

export const useServerStore = create<ServerState>((set, get) => ({
  // ---------------------------------------------------------------------------
  // Server list
  // ---------------------------------------------------------------------------

  servers: [],
  loading: true,

  fetchServers: async () => {
    try {
      const res = await api.get<Server[]>("/api/v1/servers");
      set({ servers: res.data, loading: false });
    } catch {
      set({ loading: false });
    }
  },

  // ---------------------------------------------------------------------------
  // Selected server — clears all real-time data to prevent stale state
  // ---------------------------------------------------------------------------

  selectedServerId: null,

  selectServer: (id) => {
    set({
      selectedServerId: id,
      metrics: null,
      containers: [],
      alerts: [],
    });
  },

  // ---------------------------------------------------------------------------
  // Real-time metrics
  // ---------------------------------------------------------------------------

  metrics: null,

  updateMetrics: (metrics) => {
    set({ metrics });
  },

  // ---------------------------------------------------------------------------
  // Container states
  // ---------------------------------------------------------------------------

  containers: [],

  updateContainers: (containers) => {
    set({ containers });
  },

  // ---------------------------------------------------------------------------
  // Alerts
  // ---------------------------------------------------------------------------

  alerts: [],

  addAlert: (alert) => {
    set((state) => {
      // Replace if same id exists (escalation/resolution update), else prepend
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

  // ---------------------------------------------------------------------------
  // Helpers
  // ---------------------------------------------------------------------------

  getServer: (id) => get().servers.find((s) => s.id === id),
}));
