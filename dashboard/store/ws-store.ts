"use client";

import { create } from "zustand";
import { WSClient } from "@/lib/ws";

interface WSState {
  client: WSClient | null;
  connected: boolean;
  connect: (serverId: string) => void;
  disconnect: () => void;
  send: (msg: any) => void;
  onMessage: (handler: (msg: any) => void) => () => void;
}

export const useWSStore = create<WSState>((set, get) => ({
  client: null,
  connected: false,

  connect: (serverId: string) => {
    const existing = get().client;
    if (existing) existing.disconnect();

    const client = new WSClient(serverId);
    client.onConnect(() => set({ connected: true }));
    // onclose handler in WSClient handles reconnect; we track connected state via onConnect
    // For disconnection tracking, we poll or rely on reconnect logic
    client.connect();
    set({ client });
  },

  disconnect: () => {
    get().client?.disconnect();
    set({ client: null, connected: false });
  },

  send: (msg: any) => {
    get().client?.send(msg);
  },

  onMessage: (handler) => {
    const client = get().client;
    if (!client) return () => {};
    return client.onMessage(handler);
  },
}));
