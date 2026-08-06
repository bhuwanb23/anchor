"use client";

import { useEffect, useRef } from "react";
import { LogLine } from "./log-line";

interface LogEntry {
  timestamp?: string;
  container?: string;
  message: string;
}

interface LogViewerProps {
  logs: LogEntry[];
  loading?: boolean;
}

export function LogViewer({ logs, loading }: LogViewerProps) {
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [logs.length]);

  if (loading && logs.length === 0) {
    return (
      <div className="rounded-lg bg-gray-900 p-4 font-mono text-sm text-gray-400">
        Connecting to log stream...
      </div>
    );
  }

  return (
    <div className="max-h-96 overflow-y-auto rounded-lg bg-gray-900 p-4">
      {logs.length === 0 ? (
        <p className="text-sm text-gray-500">No logs yet. Waiting for output...</p>
      ) : (
        logs.map((log, i) => (
          <LogLine key={i} timestamp={log.timestamp} container={log.container} message={log.message} />
        ))
      )}
      <div ref={bottomRef} />
    </div>
  );
}
