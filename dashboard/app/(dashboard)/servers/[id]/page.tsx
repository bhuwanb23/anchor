"use client";

import { use, useEffect } from "react";
import Link from "next/link";
import { Plus, Rocket } from "lucide-react";
import { useServer } from "@/hooks/use-server";
import { useMetrics } from "@/hooks/use-metrics";
import { useApps } from "@/hooks/use-apps";
import { useAlerts } from "@/hooks/use-alerts";
import { useServerStore } from "@/store/server-store";
import { StatusBadge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { MetricsGauges } from "@/components/server/metrics-gauges";
import { AppCard } from "@/components/app/app-card";
import { BackupStatusLine } from "@/components/server/backup-status-line";
import type { MetricsSnapshot } from "@/types";

function AlertSummary({
  serverId,
  alerts,
}: {
  serverId: string;
  alerts: { id: string; severity: string; title?: string; message: string; status: string }[];
}) {
  const active = alerts.filter((a) => a.status === "active");
  if (active.length === 0) return null;

  const critical = active.find((a) => a.severity === "critical");
  const top = critical || active[0];
  const n = active.length;

  return (
    <section className="rounded-xl border border-amber-200 bg-amber-50 p-4 dark:border-amber-900/50 dark:bg-amber-950/30">
      <h2 className="text-sm font-semibold text-amber-900 dark:text-amber-200">
        ⚠ {n} Active Alert{n === 1 ? "" : "s"}
      </h2>
      <p className="mt-2 font-medium text-gray-900 dark:text-white">
        {top.title || top.message}
      </p>
      {top.title && top.message && top.title !== top.message && (
        <p className="mt-1 text-sm text-gray-600 dark:text-gray-300">{top.message}</p>
      )}
      <Link
        href={`/servers/${serverId}/alerts`}
        className="mt-3 inline-block text-sm font-medium text-blue-600 hover:underline dark:text-blue-400"
      >
        View All Alerts →
      </Link>
    </section>
  );
}

export default function ServerOverviewPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const { server, isLoading, error } = useServer(id);
  const { metrics: polledMetrics } = useMetrics(id, 30_000);
  const storeMetrics = useServerStore((s) => s.metrics);
  const updateMetrics = useServerStore((s) => s.updateMetrics);
  const { apps, isLoading: appsLoading, refetch } = useApps(id);
  const { alerts } = useAlerts(id);

  // Seed store from REST poll when WS hasn't delivered yet
  useEffect(() => {
    if (polledMetrics && !storeMetrics) {
      const m = polledMetrics as MetricsSnapshot;
      if (m.cpu_percent !== undefined || (m as { disk_used_percent?: number }).disk_used_percent !== undefined) {
        updateMetrics({
          cpu_percent: m.cpu_percent || 0,
          ram_used_mb: m.ram_used_mb || 0,
          ram_total_mb: m.ram_total_mb || 0,
          ram_percent: m.ram_percent || 0,
          disk_used_gb: m.disk_used_gb || 0,
          disk_total_gb: m.disk_total_gb || 0,
          disk_percent:
            m.disk_percent ||
            (m as { disk_used_percent?: number }).disk_used_percent ||
            0,
          load_1min: m.load_1min || 0,
        });
      }
    }
  }, [polledMetrics, storeMetrics, updateMetrics]);

  // Keep store updated from poll every 30s as fallback
  useEffect(() => {
    if (!polledMetrics) return;
    const m = polledMetrics as MetricsSnapshot & { disk_used_percent?: number };
    updateMetrics({
      cpu_percent: m.cpu_percent || 0,
      ram_used_mb: m.ram_used_mb || 0,
      ram_total_mb: m.ram_total_mb || 0,
      ram_percent: m.ram_percent || 0,
      disk_used_gb: m.disk_used_gb || 0,
      disk_total_gb: m.disk_total_gb || 0,
      disk_percent: m.disk_percent || m.disk_used_percent || 0,
      load_1min: m.load_1min || 0,
    });
  }, [polledMetrics, updateMetrics]);

  const metrics = storeMetrics;

  if (isLoading) {
    return (
      <div className="flex items-center gap-2 text-gray-500">
        <div className="h-4 w-4 animate-spin rounded-full border-2 border-blue-600 border-t-transparent" />
        Loading server…
      </div>
    );
  }

  if (error || !server) {
    return (
      <div className="rounded-xl border border-red-200 bg-red-50 p-6 text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300">
        {error || "Server not found"}
      </div>
    );
  }

  const statusLabel =
    server.status === "connected"
      ? "Connected"
      : server.status === "error"
      ? "Error"
      : "Disconnected";

  return (
    <div className="mx-auto max-w-5xl space-y-8">
      {/* Section 1: Server status header */}
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <div className="flex flex-wrap items-center gap-3">
            <h1 className="text-3xl font-bold tracking-tight text-gray-900 dark:text-white">
              {server.name}
            </h1>
            <StatusBadge status={server.status === "error" ? "error" : server.status} />
            <span className="sr-only">{statusLabel}</span>
          </div>
          {server.agent_version && (
            <p className="mt-1 text-sm text-gray-500">Agent v{server.agent_version}</p>
          )}
        </div>
        <Link href={`/servers/${id}/apps/new`}>
          <Button size="lg">
            <Rocket className="mr-2 h-4 w-4" />
            Deploy New App
          </Button>
        </Link>
      </div>

      {/* Section 2: Resource gauges */}
      <MetricsGauges metrics={metrics} />

      {/* Section 3: Apps */}
      <section>
        <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
          <h2 className="text-lg font-semibold text-gray-900 dark:text-white">
            Apps
            <span className="ml-2 font-normal text-gray-400">· {apps.length}</span>
          </h2>
          <Link href={`/servers/${id}/apps/new`}>
            <Button size="sm" variant="secondary">
              <Plus className="mr-1 h-4 w-4" />
              New App
            </Button>
          </Link>
        </div>

        {appsLoading && apps.length === 0 ? (
          <p className="text-sm text-gray-500">Loading apps…</p>
        ) : apps.length === 0 ? (
          <div className="rounded-xl border border-dashed border-gray-200 px-6 py-10 text-center dark:border-gray-800">
            <p className="text-gray-600 dark:text-gray-300">No apps on this server yet.</p>
            <Link href={`/servers/${id}/apps/new`} className="mt-3 inline-block">
              <Button size="sm">Deploy your first app</Button>
            </Link>
          </div>
        ) : (
          <div className="grid gap-4 sm:grid-cols-2">
            {apps.map((app) => (
              <AppCard key={app.id} serverId={id} app={app} onChanged={refetch} />
            ))}
          </div>
        )}
      </section>

      {/* Section 4: Alert summary (hidden when calm) */}
      <AlertSummary serverId={id} alerts={alerts} />

      {/* Section 5: Backup status */}
      <BackupStatusLine serverId={id} />
    </div>
  );
}
