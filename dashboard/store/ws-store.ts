"use client";

import { create } from "zustand";
import { getWSClient, type BrowserWSClient } from "@/lib/ws";
import type { CommandStatus } from "@/types";

// ---------------------------------------------------------------------------
// Command progress tracking
// ---------------------------------------------------------------------------

export interface CommandProgress {
  command_id: string;
  status: CommandStatus;
  step?: string;
  message?: string;
  percent?: number;
  started_at?: string;
}

// ---------------------------------------------------------------------------
// WebSocket Store
//
// Tracks connection status and all in-progress commands. Commands are added
// when sent and removed when a result (success/failure/timeout) arrives.
// ---------------------------------------------------------------------------

interface WSState {
  // Connection status
  status: "connecting" | "connected" | "disconnected";
  client: BrowserWSClient;
  connect: () => void;
  disconnect: () => void;

  // In-progress commands
  commands: Map<string, CommandProgress>;
  addCommand: (commandId: string) => void;
  updateCommand: (commandId: string, update: Partial<Omit<CommandProgress, "command_id">>) => void;
  removeCommand: (commandId: string) => void;
}

export const useWSStore = create<WSState>((set, get) => ({
  // ---------------------------------------------------------------------------
  // Connection
  // ---------------------------------------------------------------------------

  status: "disconnected",
  client: getWSClient(),

  connect: () => {
    const { client } = get();
    client.onConnect(() => set({ status: "connected" }));
    client.onDisconnect(() => {
      // During auto-reconnect backoff, keep yellow "connecting" (Reconnecting)
      set({ status: client.isReconnecting() ? "connecting" : "disconnected" });
    });
    client.onReconnecting(() => set({ status: "connecting" }));
    set({ status: "connecting" });
    client.connect();
  },

  disconnect: () => {
    get().client.disconnect();
    set({ status: "disconnected" });
  },

  // ---------------------------------------------------------------------------
  // Command tracking
  // ---------------------------------------------------------------------------

  commands: new Map(),

  addCommand: (commandId) => {
    set((state) => {
      const next = new Map(state.commands);
      next.set(commandId, {
        command_id: commandId,
        status: "queued",
        started_at: new Date().toISOString(),
      });
      return { commands: next };
    });
  },

  updateCommand: (commandId, update) => {
    set((state) => {
      const existing = state.commands.get(commandId);
      if (!existing) return state;
      const next = new Map(state.commands);
      next.set(commandId, { ...existing, ...update });
      return { commands: next };
    });
  },

  removeCommand: (commandId) => {
    set((state) => {
      const next = new Map(state.commands);
      next.delete(commandId);
      return { commands: next };
    });
  },
}));
