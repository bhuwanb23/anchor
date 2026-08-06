"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { Bell, CheckCheck } from "lucide-react";
import { Alert } from "@/types";
import api from "@/lib/api";
import { getWSClient } from "@/lib/ws";
import { useServerStore } from "@/store/server-store";

interface AlertsResponse {
  alerts: Alert[];
  unread_count: number;
}

/**
 * Notification bell: unread count from REST + live WS bumps.
 */
export default function NotificationCenter() {
  const [open, setOpen] = useState(false);
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [loading, setLoading] = useState(true);
  const rootRef = useRef<HTMLDivElement>(null);
  const unread = useServerStore((s) => s.unreadCount);
  const setUnreadCount = useServerStore((s) => s.setUnreadCount);
  const bumpUnread = useServerStore((s) => s.bumpUnread);

  const fetchAlerts = useCallback(async (markRead: boolean) => {
    try {
      const res = await api.get<AlertsResponse>("/alerts");
      setAlerts(res.data.alerts || []);
      setUnreadCount(res.data.unread_count || 0);
      if (markRead && (res.data.unread_count || 0) > 0) {
        await api.post("/alerts/read");
        setUnreadCount(0);
      }
    } catch {
      // degrade offline
    } finally {
      setLoading(false);
    }
  }, [setUnreadCount]);

  useEffect(() => {
    fetchAlerts(false);
    const timer = setInterval(() => fetchAlerts(false), 30_000);
    return () => clearInterval(timer);
  }, [fetchAlerts]);

  // Live bump without waiting for poll
  useEffect(() => {
    const client = getWSClient();
    const onAlert = () => bumpUnread();
    const u1 = client.on("anomaly_alert", onAlert);
    const u2 = client.on("error_alert", onAlert);
    return () => {
      u1();
      u2();
    };
  }, [bumpUnread]);

  // Close when clicking outside.
  useEffect(() => {
    if (!open) return;
    const onClick = (e: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener("mousedown", onClick);
    return () => document.removeEventListener("mousedown", onClick);
  }, [open]);

  const toggle = async () => {
    const next = !open;
    setOpen(next);
    if (next) await fetchAlerts(true);
  };

  const acknowledge = async (e: React.MouseEvent, a: Alert) => {
    e.preventDefault();
    e.stopPropagation();
    try {
      await api.post(`/servers/${a.server_id}/alerts/${a.id}/ack`);
      setAlerts((prev) =>
        prev.map((x) =>
          x.id === a.id && x.status === "active"
            ? { ...x, status: "acknowledged", acknowledged_at: new Date().toISOString() }
            : x
        )
      );
    } catch {
      // Ignore; next poll reconciles.
    }
  };

  const active = alerts.filter((a) => a.status === "active");

  return (
    <div ref={rootRef} className="relative">
      <button
        onClick={toggle}
        aria-label={`Notifications (${unread} unread)`}
        className="relative rounded-lg p-2 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-gray-200"
      >
        <Bell className="h-5 w-5" />
        {unread > 0 && (
          <span className="absolute -right-0.5 -top-0.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-red-500 px-1 text-[10px] font-bold text-white">
            {unread > 9 ? "9+" : unread}
          </span>
        )}
      </button>

      {open && (
        <div className="absolute right-0 top-11 z-50 w-96 max-w-[calc(100vw-2rem)] overflow-hidden rounded-xl border border-gray-200 bg-white shadow-xl dark:border-gray-700 dark:bg-gray-900">
          <div className="flex items-center justify-between border-b border-gray-200 px-4 py-3 dark:border-gray-700">
            <span className="text-sm font-semibold text-gray-900 dark:text-white">
              Notifications
            </span>
            {unread > 0 && (
              <span className="rounded-full bg-red-100 px-2 py-0.5 text-xs font-medium text-red-600 dark:bg-red-900/40 dark:text-red-400">
                {unread} unread
              </span>
            )}
          </div>

          <div className="max-h-96 overflow-y-auto">
            {loading ? (
              <p className="px-4 py-6 text-center text-sm text-gray-400">Loading…</p>
            ) : alerts.length === 0 ? (
              <p className="px-4 py-6 text-center text-sm text-gray-400">
                No alerts — all systems normal.
              </p>
            ) : (
              alerts.slice(0, 30).map((a) => (
                <Link
                  key={a.id}
                  href={`/servers/${a.server_id}`}
                  className="block border-b border-gray-100 px-4 py-3 transition-colors hover:bg-gray-50 dark:border-gray-800 dark:hover:bg-gray-800/60"
                >
                  <div className="flex items-start justify-between gap-2">
                    <div className="min-w-0">
                      <p
                        className={`truncate text-sm font-medium ${
                          a.status === "resolved"
                            ? "text-gray-500 line-through dark:text-gray-500"
                            : a.severity === "critical"
                            ? "text-red-600 dark:text-red-400"
                            : a.severity === "warning"
                            ? "text-amber-600 dark:text-amber-400"
                            : "text-gray-600 dark:text-gray-300"
                        }`}
                      >
                        {a.title || a.message}
                      </p>
                      <p className="mt-0.5 truncate text-xs text-gray-400">
                        {a.server_name || a.server_id}
                        {a.project ? ` · ${a.project}` : ""} ·{" "}
                        {new Date(a.fired_at || a.resolved_at || Date.now()).toLocaleString()}
                      </p>
                    </div>
                    {a.status === "active" && (
                      <button
                        onClick={(e) => acknowledge(e, a)}
                        title="Acknowledge"
                        className="shrink-0 rounded-md border border-gray-200 px-1.5 py-0.5 text-xs text-gray-500 transition-colors hover:border-green-300 hover:text-green-600 dark:border-gray-600 dark:text-gray-400"
                      >
                        <CheckCheck className="h-3.5 w-3.5" />
                      </button>
                    )}
                  </div>
                </Link>
              ))
            )}
          </div>

          <div className="flex items-center justify-between border-t border-gray-200 px-4 py-2 dark:border-gray-700">
            <span className="text-xs text-gray-400">{active.length} active</span>
            <button
              type="button"
              onClick={() => api.post("/alerts/read").then(() => setUnreadCount(0))}
              className="text-xs font-medium text-blue-600 hover:text-blue-800 dark:text-blue-400"
            >
              Mark all read
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
