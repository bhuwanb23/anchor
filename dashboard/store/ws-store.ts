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
// Uses plain objects instead of Map to avoid Zustand re-render issues.
// ---------------------------------------------------------------------------

interface WSState {
  // Connection status
  status: "connecting" | "connected" | "disconnected";
  client: BrowserWSClient;
  connect: () => void;
  disconnect: () => void;

  // In-progress commands — stored as a plain object for proper Zustand equality
  commands: Record<string, CommandProgress>;
  addCommand: (commandId: string) => void;
  updateCommand: (commandId: string, update: Partial<Omit<CommandProgress, "command_id">>) => void;
  removeCommand: (commandId: string) => void;
}

// Track unsubscribe functions for cleanup
let unsubFns: (() => void)[] = [];

export const useWSStore = create<WSState>((set, get) => ({
  // ---------------------------------------------------------------------------
  // Connection
  // ---------------------------------------------------------------------------

  status: "disconnected",
  client: getWSClient(),

  connect: () => {
    // Clean up previous handlers to prevent accumulation
    unsubFns.forEach((fn) => fn());
    unsubFns = [];

    const { client } = get();
    unsubFns.push(client.onConnect(() => set({ status: "connected" })));
    unsubFns.push(client.onDisconnect(() => set({ status: "disconnected" })));
    unsubFns.push(client.onReconnecting(() => set({ status: "connecting" })));
    set({ status: "connecting" });
    client.connect();
  },

  disconnect: () => {
    unsubFns.forEach((fn) => fn());
    unsubFns = [];
    get().client.disconnect();
    set({ status: "disconnected" });
  },

  // ---------------------------------------------------------------------------
  // Command tracking — plain object for proper Zustand equality checks
  // ---------------------------------------------------------------------------

  commands: {},

  addCommand: (commandId) => {
    set((state) => ({
      commands: {
        ...state.commands,
        [commandId]: {
          command_id: commandId,
          status: "queued",
          started_at: new Date().toISOString(),
        },
      },
    }));
  },

  updateCommand: (commandId, update) => {
    set((state) => {
      const existing = state.commands[commandId];
      if (!existing) return state;
      return {
        commands: {
          ...state.commands,
          [commandId]: { ...existing, ...update },
        },
      };
    });
  },

  removeCommand: (commandId) => {
    set((state) => {
      const { [commandId]: _, ...rest } = state.commands;
      return { commands: rest };
    });
  },
}));
