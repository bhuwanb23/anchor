"use client";

import { create } from "zustand";
import { getWSClient, type BrowserWSClient } from "@/lib/ws";

interface WSState {
  client: BrowserWSClient;
  connected: boolean;
  connect: () => void;
  disconnect: () => void;
}

export const useWSStore = create<WSState>((set, get) => ({
  client: getWSClient(),
  connected: false,

  connect: () => {
    const { client } = get();
    // Listen for connection state changes
    client.onConnect(() => set({ connected: true }));
    client.onDisconnect(() => set({ connected: false }));
    client.connect();
  },

  disconnect: () => {
    get().client.disconnect();
    set({ connected: false });
  },
}));
