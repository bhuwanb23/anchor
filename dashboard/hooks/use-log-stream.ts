"use client";

import { useState, useEffect, useCallback, useMemo, useRef } from "react";
import { WSClient, WSMessage } from "@/lib/ws";
import { LogEntry } from "@/types";

// Loose shape for the union of log messages pushed by the agent (log_line,
// log_lines, log_history, stream_ended) so one handler can filter and fold
// them into LogEntry rows without TS collapsing the payload type to never.
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
  const clientRef = useRef<WSClient | null>(null);
  const MAX_LOGS = 1000;

  // Stable identity for the containers list so effect deps don't churn when a
  // caller passes a fresh array literal every render.
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

  const handleMessage = useCallback(
    (msg: WSMessage) => {
      const payload = msg.payload as LogStreamPayload | undefined;
      if (!payload) return;

      // Ignore messages for other projects/containers on the same server WS.
      if (payload.project && payload.project !== projectName) return;
      if (payload.container && !containerRoles.includes(payload.container)) {
        return;
      }

      if (msg.type === "log_line" && payload.line) {
        appendLogs([payload as LogEntry]);
      } else if (msg.type === "log_lines" && Array.isArray(payload.lines)) {
        appendLogs(payload.lines);
      } else if (msg.type === "log_history" && Array.isArray(payload.lines)) {
        prependLogs(payload.lines);
      } else if (msg.type === "stream_ended" && payload.reason) {
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
      }
    },
    [projectName, containerRoles, appendLogs, prependLogs]
  );

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

    // Send stream_logs whenever the WebSocket (re)connects. The control plane
    // also re-sends these on agent reconnect; this covers browser blips too.
    const sendStreamCommand = () => {
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
    };
    client.onConnect(sendStreamCommand);

    client.connect();

    return () => {
      unsub();
      client.disconnect();
    };
  }, [serverId, projectName, containerRoles, tail, handleMessage]);

  const stopStreaming = useCallback(() => {
    if (clientRef.current) {
      clientRef.current.send({
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
      clientRef.current.disconnect();
      clientRef.current = null;
    }
    setIsConnected(false);
  }, [projectName, containerRoles]);

  const clearLogs = useCallback(() => {
    setLogs([]);
  }, []);

  useEffect(() => {
    if (!enabled || !serverId || !projectName) return;

    const cleanup = startStreaming();

    return () => {
      // Send the selective stop while the WS is still open, then tear down.
      stopStreaming();
      cleanup?.();
    };
  }, [enabled, serverId, projectName, containersKey, startStreaming, stopStreaming]);

  return {
    logs,
    isConnected,
    error,
    startStreaming,
    stopStreaming,
    clearLogs,
  };
}
