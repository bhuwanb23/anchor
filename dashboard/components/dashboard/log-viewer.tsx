"use client";

import { useEffect, useRef } from "react";

interface LogViewerProps {
  logs: string[];
  maxHeight?: number;
}

export function LogViewer({ logs, maxHeight = 400 }: LogViewerProps) {
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight;
    }
  }, [logs]);

  return (
    <div
      ref={containerRef}
      className="overflow-auto rounded-lg border border-gray-200 bg-gray-950 p-4 font-mono text-sm text-gray-300 dark:border-gray-700"
      style={{ maxHeight }}
    >
      {logs.length === 0 ? (
        <p className="text-gray-500">No logs available</p>
      ) : (
        logs.map((line, i) => (
          <div key={i} className="whitespace-pre-wrap">
            {line}
          </div>
        ))
      )}
    </div>
  );
}
