"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { WSClient, WSMessage } from "@/lib/ws";
import { LogEntry, LogHistory } from "@/types";

interface UseLogStreamOptions {
  serverId: string;
  projectName: string;
  containers?: string[];
  tail?: number;
  enabled?: boolean;
}

interface UseLogStreamResult {
  logs: LogEntry[];
  isConnected: boolean;
  error: string | null;
  startStreaming: () => void;
  stopStreaming: () => void;
  clearLogs: () => void;
}

export function useLogStream({
  serverId,
  projectName,
  containers = ["app"],
  tail = 200,
  enabled = true,
}: UseLogStreamOptions): UseLogStreamResult {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [isConnected, setIsConnected] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const clientRef = useRef<WSClient | null>(null);
  const MAX_LOGS = 1000;

  const handleMessage = useCallback((msg: WSMessage) => {
    if (msg.type === "log_line") {
      const entry = msg.payload as LogEntry;
      setLogs((prev) => {
        const next = [...prev, entry];
        // Trim to keep memory bounded
        if (next.length > MAX_LOGS) {
          return next.slice(next.length - MAX_LOGS);
        }
        return next;
      });
    } else if (msg.type === "log_history") {
      const history = msg.payload as LogHistory;
      if (history.lines && history.lines.length > 0) {
        setLogs((prev) => {
          const combined = [...history.lines, ...prev];
          if (combined.length > MAX_LOGS) {
            return combined.slice(combined.length - MAX_LOGS);
          }
          return combined;
        });
      }
    }
  }, []);

  const startStreaming = useCallback(() => {
    if (clientRef.current) {
      clientRef.current.disconnect();
    }

    const client = new WSClient(serverId);
    clientRef.current = client;

    const unsub = client.onMessage((msg) => {
      if (msg.type === "connected") {
        setIsConnected(true);
        setError(null);
        return;
      }
      handleMessage(msg);
    });

    client.connect();

    // Send stream_logs command after a short delay to allow connection
    setTimeout(() => {
      client.send({
        type: "command",
        payload: {
          id: `stream_${Date.now()}`,
          type: "stream_logs",
          payload: {
            project_name: projectName,
            containers,
            tail,
          },
        },
      });
    }, 500);

    return () => {
      unsub();
      client.disconnect();
    };
  }, [serverId, projectName, containers, tail, handleMessage]);

  const stopStreaming = useCallback(() => {
    if (clientRef.current) {
      clientRef.current.send({
        type: "command",
        payload: {
          id: `stop_${Date.now()}`,
          type: "stop_stream_logs",
          payload: {
            all: true,
          },
        },
      });
      clientRef.current.disconnect();
      clientRef.current = null;
    }
    setIsConnected(false);
  }, []);

  const clearLogs = useCallback(() => {
    setLogs([]);
  }, []);

  useEffect(() => {
    if (!enabled || !serverId || !projectName) return;

    const cleanup = startStreaming();

    return () => {
      cleanup?.();
      stopStreaming();
    };
  }, [enabled, serverId, projectName]);

  return {
    logs,
    isConnected,
    error,
    startStreaming,
    stopStreaming,
    clearLogs,
  };
}
