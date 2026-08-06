"use client";

import { useState, useEffect, useCallback, useMemo, useRef } from "react";
import { getWSClient, type WSMessage } from "@/lib/ws";
import type { LogEntry, LogStream } from "@/types";

export type LogConnectionStatus = "live" | "reconnecting" | "stopped" | "idle";

interface LogStreamPayload {
  project?: string;
  container?: string;
  stream?: LogStream;
  line?: string;
  timestamp?: string;
  lines?: LogEntry[];
  reason?: string;
  type?: string;
}

interface UseLogStreamOptions {
  serverId: string;
  projectName: string;
  containers?: string[];
  tail?: number;
  enabled?: boolean;
  maxLines?: number;
}

interface UseLogStreamResult {
  logs: LogEntry[];
  status: LogConnectionStatus;
  isConnected: boolean;
  error: string | null;
  startStreaming: () => void;
  stopStreaming: () => void;
  clearLogs: () => void;
}

const STOP_MARKER = "--- Container stopped ---";

function normalizeEntry(raw: Partial<LogEntry> & { line?: string; message?: string }): LogEntry {
  return {
    type: "log_line",
    project: raw.project || "",
    container: raw.container || "",
    stream: raw.stream === "stderr" ? "stderr" : "stdout",
    line: raw.line || (raw as { message?: string }).message || "",
    timestamp: raw.timestamp || new Date().toISOString(),
  };
}

export function useLogStream({
  serverId,
  projectName,
  containers = ["app"],
  tail = 200,
  enabled = true,
  maxLines = 2000,
}: UseLogStreamOptions): UseLogStreamResult {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [status, setStatus] = useState<LogConnectionStatus>("idle");
  const [error, setError] = useState<string | null>(null);
  const cleanupRef = useRef<(() => void) | null>(null);

  const containersKey = containers.join(",");
  const containerRoles = useMemo(
    () => containersKey.split(",").filter(Boolean),
    [containersKey]
  );

  const appendLogs = useCallback(
    (entries: LogEntry[]) => {
      if (entries.length === 0) return;
      setLogs((prev) => {
        const next = [...prev, ...entries];
        return next.length > maxLines ? next.slice(next.length - maxLines) : next;
      });
    },
    [maxLines]
  );

  const replaceWithHistory = useCallback(
    (entries: LogEntry[]) => {
      const sliced =
        entries.length > maxLines ? entries.slice(entries.length - maxLines) : entries;
      setLogs(sliced);
    },
    [maxLines]
  );

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
    // Also try start_log_stream alias used by some agents
    client.send({
      type: "start_log_stream",
      payload: {
        project_name: projectName,
        containers: containerRoles,
        tail,
      },
    });
  }, [projectName, containerRoles, tail]);

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
          all: false,
        },
      },
    });
    client.send({
      type: "stop_log_stream",
      payload: {
        project_name: projectName,
        containers: containerRoles,
      },
    });
    setStatus((s) => (s === "stopped" ? s : "idle"));
  }, [projectName, containerRoles]);

  const clearLogs = useCallback(() => {
    setLogs([]);
  }, []);

  const startStreaming = useCallback(() => {
    cleanupRef.current?.();
    const client = getWSClient();
    setStatus("reconnecting");
    setError(null);

    const unsubConnect = client.onConnect(() => {
      setStatus("live");
      sendStreamCommand();
    });

    const unsubDisconnect = client.onDisconnect(() => {
      setStatus((s) => (s === "stopped" ? s : "reconnecting"));
    });

    client.subscribeServer(serverId);

    const matchesProject = (p?: LogStreamPayload) =>
      !p?.project || p.project === projectName;

    const matchesContainer = (p?: LogStreamPayload) => {
      if (!p?.container) return true;
      // Accept role names or container name containing role
      return containerRoles.some(
        (role) => p.container === role || (p.container || "").includes(role)
      );
    };

    const unsubLogLine = client.on("log_line", (msg: WSMessage) => {
      const payload = (msg.payload || msg) as LogStreamPayload;
      if (!payload?.line && !(payload as { message?: string }).message) return;
      if (!matchesProject(payload) || !matchesContainer(payload)) return;
      setStatus("live");
      appendLogs([normalizeEntry(payload)]);
    });

    const unsubLogLines = client.on("log_lines", (msg: WSMessage) => {
      const payload = (msg.payload || msg) as LogStreamPayload;
      if (!payload?.lines?.length) return;
      if (!matchesProject(payload) || !matchesContainer(payload)) return;
      setStatus("live");
      appendLogs(payload.lines.map(normalizeEntry));
    });

    const onHistory = (msg: WSMessage) => {
      const payload = (msg.payload || msg) as LogStreamPayload;
      if (!payload?.lines) return;
      if (!matchesProject(payload) || !matchesContainer(payload)) return;
      setStatus("live");
      replaceWithHistory(payload.lines.map(normalizeEntry));
    };

    const unsubHistory = client.on("log_history", onHistory);
    const unsubInitial = client.on("initial_logs", onHistory);

    const unsubStreamEnded = client.on("stream_ended", (msg: WSMessage) => {
      const payload = (msg.payload || msg) as LogStreamPayload;
      if (!matchesProject(payload)) return;
      setStatus("stopped");
      appendLogs([
        {
          type: "log_line",
          project: payload.project ?? projectName,
          container: payload.container ?? containerRoles[0] ?? "",
          stream: "stdout",
          line: STOP_MARKER,
          timestamp: new Date().toISOString(),
        },
      ]);
    });

    client.connect();
    if (client /* already open */) {
      // Send immediately; onConnect will re-send if reconnecting
      sendStreamCommand();
      setStatus("live");
    }

    const cleanup = () => {
      unsubConnect();
      unsubDisconnect();
      unsubLogLine();
      unsubLogLines();
      unsubHistory();
      unsubInitial();
      unsubStreamEnded();
      stopStreaming();
    };
    cleanupRef.current = cleanup;
    return cleanup;
  }, [
    serverId,
    projectName,
    containerRoles,
    sendStreamCommand,
    appendLogs,
    replaceWithHistory,
    stopStreaming,
  ]);

  useEffect(() => {
    if (!enabled || !serverId || !projectName) return;
    clearLogs();
    const cleanup = startStreaming();
    return () => {
      cleanup?.();
      cleanupRef.current = null;
    };
  }, [enabled, serverId, projectName, containersKey, startStreaming, clearLogs]);

  return {
    logs,
    status,
    isConnected: status === "live",
    error,
    startStreaming,
    stopStreaming,
    clearLogs,
  };
}

export { STOP_MARKER };
