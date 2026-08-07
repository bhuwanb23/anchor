"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { ChevronDown } from "lucide-react";
import { LogLine } from "./log-line";
import type { LogEntry } from "@/types";
import type { LogConnectionStatus } from "@/hooks/use-log-stream";

export type ContainerRole = "app" | "postgres" | "redis";

const CONTAINER_OPTIONS: { value: ContainerRole; label: string }[] = [
  { value: "app", label: "App" },
  { value: "postgres", label: "Postgres" },
  { value: "redis", label: "Redis" },
];

interface LogViewerProps {
  logs: LogEntry[];
  status?: LogConnectionStatus;
  container?: ContainerRole;
  onContainerChange?: (role: ContainerRole) => void;
  showContainerSelector?: boolean;
  className?: string;
  /** Full-page mode fills remaining viewport height */
  fill?: boolean;
}

function statusLabel(status: LogConnectionStatus): { text: string; className: string } {
  switch (status) {
    case "live":
      return { text: "● Live", className: "text-green-400" };
    case "reconnecting":
      return { text: "● Reconnecting", className: "animate-pulse text-yellow-400" };
    case "stopped":
      return { text: "○ Stopped", className: "text-gray-500" };
    default:
      return { text: "○ Idle", className: "text-gray-500" };
  }
}

export function LogViewer({
  logs,
  status = "idle",
  container = "app",
  onContainerChange,
  showContainerSelector = false,
  className = "",
  fill = false,
}: LogViewerProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const bottomRef = useRef<HTMLDivElement>(null);
  const stickToBottom = useRef(true);
  const [showJump, setShowJump] = useState(false);

  const jumpToLatest = useCallback(() => {
    stickToBottom.current = true;
    setShowJump(false);
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, []);

  const onScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    const atBottom = distanceFromBottom < 40;
    stickToBottom.current = atBottom;
    setShowJump(!atBottom);
  }, []);

  useEffect(() => {
    if (!stickToBottom.current) {
      setShowJump(true);
      return;
    }
    bottomRef.current?.scrollIntoView({ behavior: "auto" });
    setShowJump(false);
  }, [logs.length]);

  const ind = statusLabel(status);

  return (
    <div className={`flex flex-col ${fill ? "min-h-0 flex-1" : ""} ${className}`}>
      {/* Top bar */}
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-t-lg border border-b-0 border-gray-800 bg-gray-950 px-3 py-2">
        {showContainerSelector ? (
          <label className="flex items-center gap-2 text-sm text-gray-300">
            <span className="sr-only">Container</span>
            <select
              value={container}
              onChange={(e) => onContainerChange?.(e.target.value as ContainerRole)}
              className="rounded-md border border-gray-700 bg-gray-900 px-2 py-1 text-sm text-gray-100"
            >
              {CONTAINER_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </select>
          </label>
        ) : (
          <span className="text-sm capitalize text-gray-400">{container}</span>
        )}
        <span className={`text-xs font-medium ${ind.className}`}>{ind.text}</span>
      </div>

      {/* Log area */}
      <div className="relative min-h-0 flex-1">
        <div
          ref={scrollRef}
          onScroll={onScroll}
          className={`overflow-auto rounded-b-lg border border-gray-800 bg-gray-950 p-3 ${
            fill ? "h-full min-h-[20rem]" : "max-h-[28rem]"
          }`}
        >
          {logs.length === 0 ? (
            <p className="font-mono text-sm text-gray-600">Waiting for logs…</p>
          ) : (
            logs.map((log, i) => (
              <LogLine
                key={`${log.timestamp}-${i}`}
                timestamp={log.timestamp}
                container={log.container}
                message={log.line}
                stream={log.stream}
              />
            ))
          )}
          <div ref={bottomRef} />
        </div>

        {showJump && (
          <button
            type="button"
            onClick={jumpToLatest}
            className="absolute bottom-4 right-4 flex items-center gap-1 rounded-full bg-[var(--color-accent)] px-3 py-1.5 text-xs font-semibold text-[var(--color-accent-fg)] shadow-lg hover:bg-[var(--color-accent-hover)]"
          >
            <ChevronDown className="h-3.5 w-3.5" />
            Jump to latest
          </button>
        )}
      </div>
    </div>
  );
}
