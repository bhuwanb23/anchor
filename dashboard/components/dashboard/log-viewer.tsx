"use client";

import { useEffect, useRef } from "react";
import { LogEntry } from "@/types";

interface LogViewerProps {
  logs: LogEntry[];
  maxHeight?: number;
  showContainerPrefix?: boolean;
}

export function LogViewer({
  logs,
  maxHeight = 400,
  showContainerPrefix = false,
}: LogViewerProps) {
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight;
    }
  }, [logs]);

  const formatTimestamp = (ts: string) => {
    try {
      const d = new Date(ts);
      return d.toLocaleTimeString();
    } catch {
      return ts;
    }
  };

  return (
    <div
      ref={containerRef}
      className="overflow-auto rounded-lg border border-gray-200 bg-gray-950 p-4 font-mono text-sm text-gray-300 dark:border-gray-700"
      style={{ maxHeight }}
    >
      {logs.length === 0 ? (
        <p className="text-gray-500">No logs available</p>
      ) : (
        logs.map((entry, i) => (
          <div key={i} className="flex gap-2 whitespace-pre-wrap">
            <span className="select-none text-gray-600 shrink-0">
              {formatTimestamp(entry.timestamp)}
            </span>
            {showContainerPrefix && (
              <span
                className={`select-none shrink-0 ${
                  entry.container === "app"
                    ? "text-blue-400"
                    : entry.container === "postgres"
                    ? "text-indigo-400"
                    : entry.container === "redis"
                    ? "text-red-400"
                    : "text-gray-400"
                }`}
              >
                [{entry.container}]
              </span>
            )}
            <span
              className={
                entry.stream === "stderr" ? "text-red-400" : "text-gray-300"
              }
            >
              {entry.line}
            </span>
          </div>
        ))
      )}
    </div>
  );
}
