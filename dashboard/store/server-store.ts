"use client";

import { create } from "zustand";
import api from "@/lib/api";

interface Server {
  id: string;
  name: string;
  status: string;
  connected_at?: string;
  last_seen?: string;
  os_info?: string;
  arch?: string;
  ram_mb?: number;
  disk_gb?: number;
  ip_address?: string;
  metrics?: {
    cpu_percent?: number;
    ram_used_mb?: number;
    ram_total_mb?: number;
    ram_percent?: number;
    disk_used_gb?: number;
    disk_total_gb?: number;
  };
}

interface ServerState {
  servers: Server[];
  loading: boolean;
  fetchServers: () => Promise<void>;
  getServer: (id: string) => Server | undefined;
}

export const useServerStore = create<ServerState>((set, get) => ({
  servers: [],
  loading: true,

  fetchServers: async () => {
    try {
      const res = await api.get("/api/v1/servers");
      set({ servers: res.data, loading: false });
    } catch {
      set({ loading: false });
    }
  },

  getServer: (id) => get().servers.find((s) => s.id === id),
}));
