"use client";

import { useState, useEffect, useCallback, useMemo, useRef } from "react";
import { getWSClient, type WSMessage } from "@/lib/ws";
import { LogEntry } from "@/types";

interface LogStreamPayload extends Partial<LogEntry> {
  lines?: LogEntry[];
  reason?: string;
}

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
  const MAX_LOGS = 1000;

  const containersKey = containers.join(",");
  const containerRoles = useMemo(
    () => containersKey.split(","),
    [containersKey]
  );

  const appendLogs = useCallback((entries: LogEntry[]) => {
    if (entries.length === 0) return;
    setLogs((prev) => {
      const next = [...prev, ...entries];
      return next.length > MAX_LOGS ? next.slice(next.length - MAX_LOGS) : next;
    });
  }, []);

  const prependLogs = useCallback((entries: LogEntry[]) => {
    if (entries.length === 0) return;
    setLogs((prev) => {
      const combined = [...entries, ...prev];
      return combined.length > MAX_LOGS
        ? combined.slice(combined.length - MAX_LOGS)
        : combined;
    });
  }, []);

  const sendStreamCommand = useCallback(() => {
    const client = getWSClient();
    client.send({
      type: "command",
      payload: {
        id: `stream_${Date.now()}`,
        type: "stream_logs",
        payload: {
          project_name: projectName,
          containers: containerRoles,
          tail,
        },
      },
    });
  }, [projectName, containerRoles, tail]);

  const startStreaming = useCallback(() => {
    const client = getWSClient();

    // Re-send stream command on reconnect
    const unsubConnect = client.onConnect(() => {
      setIsConnected(true);
      sendStreamCommand();
    });

    const unsubDisconnect = client.onDisconnect(() => {
      setIsConnected(false);
    });

    // Subscribe to this server
    client.subscribeServer(serverId);

    // Register message handlers
    const unsubConnected = client.on("connected", () => {
      setIsConnected(true);
      setError(null);
    });

    const unsubLogLine = client.on("log_line", (msg: WSMessage) => {
      const payload = msg.payload as LogStreamPayload | undefined;
      if (!payload?.line) return;
      if (payload.project && payload.project !== projectName) return;
      if (payload.container && !containerRoles.includes(payload.container)) return;
      appendLogs([payload as LogEntry]);
    });

    const unsubLogLines = client.on("log_lines", (msg: WSMessage) => {
      const payload = msg.payload as LogStreamPayload | undefined;
      if (!payload?.lines) return;
      if (payload.project && payload.project !== projectName) return;
      if (payload.container && !containerRoles.includes(payload.container)) return;
      appendLogs(payload.lines);
    });

    const unsubLogHistory = client.on("log_history", (msg: WSMessage) => {
      const payload = msg.payload as LogStreamPayload | undefined;
      if (!payload?.lines) return;
      if (payload.project && payload.project !== projectName) return;
      if (payload.container && !containerRoles.includes(payload.container)) return;
      prependLogs(payload.lines);
    });

    const unsubStreamEnded = client.on("stream_ended", (msg: WSMessage) => {
      const payload = msg.payload as LogStreamPayload | undefined;
      if (!payload) return;
      if (payload.project && payload.project !== projectName) return;
      appendLogs([
        {
          type: "log_line",
          project: payload.project ?? "",
          container: payload.container ?? "",
          stream: "stdout",
          line:
            payload.reason === "read_error"
              ? "[Log stream error]"
              : "[Container stopped]",
          timestamp: new Date().toISOString(),
        },
      ]);
    });

    // Ensure connection
    client.connect();

    // Send initial stream command
    sendStreamCommand();

    // Return cleanup function
    return () => {
      unsubConnect();
      unsubDisconnect();
      unsubConnected();
      unsubLogLine();
      unsubLogLines();
      unsubLogHistory();
      unsubStreamEnded();
    };
  }, [serverId, projectName, containerRoles, tail, appendLogs, prependLogs, sendStreamCommand]);

  const stopStreaming = useCallback(() => {
    const client = getWSClient();
    client.send({
      type: "command",
      payload: {
        id: `stop_${Date.now()}`,
        type: "stop_stream_logs",
        payload: {
          project_name: projectName,
          containers: containerRoles,
        },
      },
    });
    setIsConnected(false);
  }, [projectName, containerRoles]);

  const clearLogs = useCallback(() => {
    setLogs([]);
  }, []);

  useEffect(() => {
    if (!enabled || !serverId || !projectName) return;
    const cleanup = startStreaming();
    return () => {
      cleanup?.();
    };
  }, [enabled, serverId, projectName, containersKey, startStreaming]);

  return {
    logs,
    isConnected,
    error,
    startStreaming,
    stopStreaming,
    clearLogs,
  };
}
