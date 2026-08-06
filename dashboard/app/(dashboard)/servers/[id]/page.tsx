"use client";

import { use, useCallback, useEffect, useState } from "react";
import { useServer } from "@/hooks/use-server";
import { useLogStream } from "@/hooks/use-log-stream";
import { useAlerts } from "@/hooks/use-alerts";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { StatusBadge } from "@/components/ui/badge";
import { LogViewer } from "@/components/dashboard/log-viewer";
import { ServerEvent } from "@/types";
import api from "@/lib/api";
import { ArrowLeft, Server, HardDrive, Cpu, Wifi, Activity, Sparkles } from "lucide-react";
import Link from "next/link";

export default function ServerDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const { server, isLoading, error } = useServer(id);
  const [projectName, setProjectName] = useState("");
  const [showLogs, setShowLogs] = useState(false);

  const {
    logs,
    isConnected: logConnected,
    startStreaming,
    stopStreaming,
    clearLogs,
  } = useLogStream({
    serverId: id,
    projectName,
    enabled: showLogs && !!projectName,
  });

  const { alerts, acknowledge } = useAlerts(id);

  // Layer 4C Step 7 — history of automatic actions the agent took.
  const [actions, setActions] = useState<ServerEvent[]>([]);
  const fetchActions = useCallback(() => {
    api
      .get<ServerEvent[]>(`/servers/${id}/events`)
      .then((res) => {
        setActions((res.data || []).filter((e) => e.event_type === "auto_remediation").slice(0, 20));
      })
      .catch(() => {});
  }, [id]);
  useEffect(() => {
    fetchActions();
    const t = setInterval(fetchActions, 60_000);
    return () => clearInterval(t);
  }, [fetchActions]);

  if (isLoading) {
    return (
      <div className="flex items-center gap-2 text-gray-500">
        <Server className="h-4 w-4 animate-pulse" />
        Loading server...
      </div>
    );
  }

  if (error || !server) {
    return (
      <div className="space-y-4">
        <Link
          href="/servers"
          className="inline-flex items-center gap-2 text-sm text-gray-500 hover:text-gray-700"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to servers
        </Link>
        <Card>
          <CardContent className="py-12 text-center text-red-500">
            {error || "Server not found"}
          </CardContent>
        </Card>
      </div>
    );
  }

  const lastSeen = server.last_seen ? new Date(server.last_seen).toLocaleString() : "N/A";
  const connectedAt = server.connected_at ? new Date(server.connected_at).toLocaleString() : "N/A";

  return (
    <div className="space-y-6">
      <Link
        href="/servers"
        className="inline-flex items-center gap-2 text-sm text-gray-500 hover:text-gray-700"
      >
        <ArrowLeft className="h-4 w-4" />
        Back to servers
      </Link>

      <div className="flex items-center gap-4">
        <h1 className="text-2xl font-bold text-gray-900 dark:text-white">{server.name}</h1>
        <StatusBadge status={server.status} />
        <Link
          href={`/servers/${id}/backups`}
          className="text-sm text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
        >
          Backups
        </Link>
      </div>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium text-gray-500">Server ID</CardTitle>
          </CardHeader>
          <CardContent>
            <code className="text-sm text-gray-900 dark:text-white">{server.id}</code>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium text-gray-500">Status</CardTitle>
          </CardHeader>
          <CardContent>
            <StatusBadge status={server.status} />
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium text-gray-500">IP Address</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-2">
              <Wifi className="h-4 w-4 text-gray-400" />
              <span className="text-sm text-gray-900 dark:text-white">{server.ip_address || "N/A"}</span>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium text-gray-500">Operating System</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-2">
              <Server className="h-4 w-4 text-gray-400" />
              <span className="text-sm text-gray-900 dark:text-white">
                {server.os_pretty || (server.os_info && server.os_version ? `${server.os_info} ${server.os_version}` : server.os_info || "N/A")}
              </span>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium text-gray-500">Architecture</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-2">
              <Cpu className="h-4 w-4 text-gray-400" />
              <span className="text-sm text-gray-900 dark:text-white">{server.arch || "N/A"}</span>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium text-gray-500">Memory</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-2">
              <Activity className="h-4 w-4 text-gray-400" />
              <span className="text-sm text-gray-900 dark:text-white">
                {server.ram_mb ? `${server.ram_mb} MB` : "N/A"}
                {server.ram_available_mb ? ` (${server.ram_available_mb} MB free)` : ""}
              </span>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium text-gray-500">Disk</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-2">
              <HardDrive className="h-4 w-4 text-gray-400" />
              <span className="text-sm text-gray-900 dark:text-white">
                {server.disk_total_gb ? `${server.disk_total_gb} GB` : server.disk_gb ? `${server.disk_gb} GB` : "N/A"}
                {server.disk_available_gb ? ` (${server.disk_available_gb} GB free)` : ""}
              </span>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium text-gray-500">Docker</CardTitle>
          </CardHeader>
          <CardContent>
            <span className="text-sm text-gray-900 dark:text-white">{server.docker_version || "N/A"}</span>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium text-gray-500">Connected At</CardTitle>
          </CardHeader>
          <CardContent>
            <span className="text-sm text-gray-900 dark:text-white">{connectedAt}</span>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium text-gray-500">Last Seen</CardTitle>
          </CardHeader>
          <CardContent>
            <span className="text-sm text-gray-900 dark:text-white">{lastSeen}</span>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle>Logs</CardTitle>
            <div className="flex items-center gap-2">
              <input
                type="text"
                placeholder="Project name"
                value={projectName}
                onChange={(e) => setProjectName(e.target.value)}
                className="rounded border border-gray-300 px-2 py-1 text-sm dark:border-gray-600 dark:bg-gray-800"
              />
              {showLogs ? (
                <button
                  onClick={() => {
                    stopStreaming();
                    setShowLogs(false);
                    clearLogs();
                  }}
                  className="rounded bg-red-600 px-3 py-1 text-sm text-white hover:bg-red-700"
                >
                  Stop
                </button>
              ) : (
                <button
                  onClick={() => {
                    if (projectName) {
                      setShowLogs(true);
                    }
                  }}
                  disabled={!projectName}
                  className="rounded bg-blue-600 px-3 py-1 text-sm text-white hover:bg-blue-700 disabled:opacity-50"
                >
                  View Logs
                </button>
              )}
              {logConnected && (
                <span className="text-xs text-green-500">Streaming</span>
              )}
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <LogViewer logs={logs} showContainerPrefix />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle className="text-sm font-medium">Alerts</CardTitle>
            <span className="text-xs text-gray-400">
              {alerts.filter((a) => a.status === "active").length} active
            </span>
          </div>
        </CardHeader>
        <CardContent>
          {alerts.length === 0 ? (
            <p className="text-sm text-gray-500">
              No alerts — all systems normal.
            </p>
          ) : (
            <div className="max-h-96 space-y-2 overflow-y-auto pr-1">
              {alerts.map((a) => (
                <div
                  key={a.id}
                  className={`rounded-r border-l-4 p-3 ${
                    a.status === "resolved" || a.level === "resolved"
                      ? "border-green-500 bg-green-50 dark:bg-green-950/40"
                      : a.severity === "critical" || a.level === "critical"
                      ? "border-red-500 bg-red-50 dark:bg-red-950/40"
                      : "border-amber-500 bg-amber-50 dark:bg-amber-950/40"
                  }`}
                >
                  <div className="flex items-center justify-between gap-2">
                    <span
                      className={`text-xs font-semibold uppercase tracking-wide ${
                        a.status === "resolved" || a.level === "resolved"
                          ? "text-green-600 dark:text-green-400"
                          : a.severity === "critical" || a.level === "critical"
                          ? "text-red-600 dark:text-red-400"
                          : "text-amber-600 dark:text-amber-400"
                      }`}
                    >
                      {a.status === "resolved" || a.level === "resolved" ? "✓ Resolved" : a.status === "active" ? a.severity || a.level : a.level}
                      {a.project ? ` · ${a.project}${a.container ? ` (${a.container})` : ""}` : ""}
                    </span>
                    <span className="shrink-0 text-xs text-gray-400">
                      {new Date(a.at).toLocaleString()}
                    </span>
                  </div>
                  <p className="mt-1 text-sm font-medium text-gray-800 dark:text-gray-200">
                    {a.title || a.message}
                  </p>
                  {a.message && a.title && a.message !== a.title && (
                    <p className="mt-1 text-sm text-gray-600 dark:text-gray-300">
                      {a.message}
                    </p>
                  )}
                  {a.detail && (
                    <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
                      {a.detail}
                    </p>
                  )}
                  {a.action && a.status === "active" && (
                    <p className="mt-2 rounded bg-white/60 p-2 text-xs italic text-gray-600 dark:bg-gray-900/30 dark:text-gray-400">
                      💡 {a.action}
                    </p>
                  )}
                  {a.status === "active" && (
                    <div className="mt-2 flex justify-end">
                      <button
                        onClick={() => acknowledge(a.id)}
                        className="rounded-md border border-gray-300 px-2 py-1 text-xs font-medium text-gray-600 transition-colors hover:border-green-400 hover:text-green-600 dark:border-gray-600 dark:text-gray-300"
                      >
                        Acknowledge
                      </button>
                    </div>
                  )}
                  {a.status === "acknowledged" && (
                    <p className="mt-1 text-right text-xs text-gray-400">
                      ✓ Acknowledged
                      {a.acknowledged_at
                        ? ` ${new Date(a.acknowledged_at).toLocaleString()}`
                        : ""}
                    </p>
                  )}
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle className="flex items-center gap-2 text-sm font-medium">
              <Sparkles className="h-4 w-4 text-violet-500" />
              Automatic Actions
            </CardTitle>
            <span className="text-xs text-gray-400">
              What the agent fixed on its own
            </span>
          </div>
        </CardHeader>
        <CardContent>
          {actions.length === 0 ? (
            <p className="text-sm text-gray-500">
              No automatic actions taken yet — the agent will clean up disk space, restart
              the reverse proxy, and recover crashed apps on its own when needed.
            </p>
          ) : (
            <div className="max-h-72 space-y-2 overflow-y-auto pr-1">
              {actions.map((e) => (
                <div
                  key={e.id}
                  className="flex items-start justify-between gap-3 rounded-r border-l-4 border-violet-400 bg-violet-50 p-3 dark:bg-violet-950/40"
                >
                  <div className="min-w-0">
                    <p className="text-sm font-medium text-gray-800 dark:text-gray-200">
                      {e.message || e.check_name}
                    </p>
                    {e.check_name && e.check_name !== e.message && (
                      <p className="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                        Action: {e.check_name}
                      </p>
                    )}
                  </div>
                  <span className="shrink-0 text-xs text-gray-400">
                    {new Date(e.created_at).toLocaleString()}
                  </span>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
