"use client";

import type { LogStream } from "@/types";

interface LogLineProps {
  timestamp?: string;
  container?: string;
  message: string;
  stream?: LogStream | string;
}

function formatLocalTime(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleTimeString(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
}

export function LogLine({ timestamp, container, message, stream }: LogLineProps) {
  const isStderr = stream === "stderr";
  const isMarker = message.startsWith("--- ");

  return (
    <div className="flex gap-3 font-mono text-xs leading-5 select-text">
      {timestamp && (
        <span className="w-[4.5rem] shrink-0 tabular-nums text-gray-500">
          {formatLocalTime(timestamp)}
        </span>
      )}
      {container && !isMarker && (
        <span className="w-16 shrink-0 truncate text-gray-500">[{container}]</span>
      )}
      <span
        className={`min-w-0 flex-1 break-all whitespace-pre-wrap ${
          isMarker
            ? "italic text-gray-500"
            : isStderr
            ? "text-orange-400"
            : "text-gray-200"
        }`}
      >
        {message}
      </span>
    </div>
  );
}
