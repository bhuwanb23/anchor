"use client";

import { use, useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Plus, Rocket, Trash2 } from "lucide-react";
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
import {
  FadeIn,
  PageError,
  ServerDisconnectedBanner,
  ServerOverviewSkeleton,
} from "@/components/ui/page-states";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import api from "@/lib/api";
import { toast } from "sonner";
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
    <section className="rounded-[var(--radius-lg)] border border-[var(--color-warning)]/30 bg-[var(--color-warning-soft)] p-4">
      <h2 className="text-sm font-semibold text-[var(--color-warning)]">
        {n} Active Alert{n === 1 ? "" : "s"}
      </h2>
      <p className="mt-2 font-medium text-[var(--color-ink)]">
        {top.title || top.message}
      </p>
      {top.title && top.message && top.title !== top.message && (
        <p className="mt-1 text-sm text-[var(--color-muted)]">{top.message}</p>
      )}
      <Link
        href={`/servers/${serverId}/alerts`}
        className="mt-3 inline-block text-sm font-semibold text-[var(--color-accent)] hover:underline"
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
  const router = useRouter();
  const { server, isLoading, error } = useServer(id);
  const { metrics: polledMetrics } = useMetrics(id, 30_000);
  const storeMetrics = useServerStore((s) => s.metrics);
  const updateMetrics = useServerStore((s) => s.updateMetrics);
  const { apps, isLoading: appsLoading, refetch } = useApps(id);
  const { alerts } = useAlerts(id);
  const [showSkel, setShowSkel] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteConfirm, setDeleteConfirm] = useState("");

  // Seed store from REST poll when WS hasn't delivered yet
  useEffect(() => {
    if (polledMetrics && !storeMetrics) {
      const m = polledMetrics as MetricsSnapshot & { disk_used_percent?: number };
      updateMetrics({
        cpu_percent: m.cpu_percent ?? 0,
        ram_used_mb: m.ram_used_mb ?? 0,
        ram_total_mb: m.ram_total_mb ?? 0,
        ram_percent: m.ram_percent ?? 0,
        disk_used_gb: m.disk_used_gb ?? 0,
        disk_total_gb: m.disk_total_gb ?? 0,
        disk_percent: m.disk_percent ?? m.disk_used_percent ?? 0,
        load_1min: m.load_1min ?? 0,
      });
    }
  }, [polledMetrics, storeMetrics, updateMetrics]);

  // Keep store updated from poll every 30s as fallback
  useEffect(() => {
    if (!polledMetrics) return;
    const m = polledMetrics as MetricsSnapshot & { disk_used_percent?: number };
    updateMetrics({
      cpu_percent: m.cpu_percent ?? 0,
      ram_used_mb: m.ram_used_mb ?? 0,
      ram_total_mb: m.ram_total_mb ?? 0,
      ram_percent: m.ram_percent ?? 0,
      disk_used_gb: m.disk_used_gb ?? 0,
      disk_total_gb: m.disk_total_gb ?? 0,
      disk_percent: m.disk_percent ?? m.disk_used_percent ?? 0,
      load_1min: m.load_1min ?? 0,
    });
  }, [polledMetrics, updateMetrics]);

  useEffect(() => {
    if (!isLoading) {
      setShowSkel(false);
      return;
    }
    const t = setTimeout(() => setShowSkel(true), 200);
    return () => clearTimeout(t);
  }, [isLoading]);

  const metrics = storeMetrics;

  if (isLoading && showSkel) {
    return <ServerOverviewSkeleton />;
  }
  if (isLoading) return null;

  if (error || !server) {
    return (
      <PageError
        message={
          error?.toLowerCase().includes("not found") || error?.toLowerCase().includes("403")
            ? "This server was not found, or you do not have access to it."
            : "We could not load your server information. Try again in a moment."
        }
        onRetry={() => window.location.reload()}
      />
    );
  }

  const disconnected = server.status !== "connected";

  const deleteServer = async () => {
    try {
      await api.delete(`/api/v1/servers/${id}`);
      toast.success("Server removed");
      router.push("/servers");
    } catch (e) {
      const msg =
        (e as { response?: { data?: { error?: string } } })?.response?.data?.error ||
        (e instanceof Error ? e.message : "Could not delete server");
      toast.error(msg);
      setDeleteOpen(false);
    }
  };

  return (
    <FadeIn>
    <div className="mx-auto max-w-5xl space-y-8">
      {/* Section 1: Server status header */}
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <div className="flex flex-wrap items-center gap-3">
            <h1 className="text-3xl font-extrabold tracking-tight text-[var(--color-ink)]">
              {server.name}
            </h1>
            <StatusBadge status={server.status === "error" ? "error" : server.status} />
          </div>
          {server.agent_version && (
            <p className="mt-1 text-sm text-[var(--color-muted)]">Agent v{server.agent_version}</p>
          )}
        </div>
        <div className="flex flex-wrap gap-2">
          <Link href={`/servers/${id}/apps/new`}>
            <Button size="lg" className="gap-2">
              <Rocket className="h-4 w-4" />
              Deploy New App
            </Button>
          </Link>
          <Button
            size="lg"
            variant="secondary"
            className="gap-2"
            onClick={() => {
              setDeleteConfirm("");
              setDeleteOpen(true);
            }}
          >
            <Trash2 className="h-4 w-4" />
            Delete server
          </Button>
        </div>
      </div>

      {disconnected && (
        <ServerDisconnectedBanner lastSeen={server.last_seen} />
      )}

      {/* Section 2: Resource gauges */}
      <MetricsGauges metrics={metrics} />

      {/* Section 3: Apps */}
      <section>
        <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
          <h2 className="text-lg font-bold tracking-tight text-[var(--color-ink)]">
            Apps
            <span className="ml-2 font-normal text-[var(--color-muted)]">· {apps.length}</span>
          </h2>
          <Link href={`/servers/${id}/apps/new`}>
            <Button size="sm" variant="secondary" className="gap-1">
              <Plus className="h-4 w-4" />
              New App
            </Button>
          </Link>
        </div>

        {appsLoading && apps.length === 0 ? (
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="h-32 animate-pulse rounded-[var(--radius-lg)] bg-[var(--color-paper-2)]" />
            <div className="h-32 animate-pulse rounded-[var(--radius-lg)] bg-[var(--color-paper-2)]" />
          </div>
        ) : apps.length === 0 ? (
          <div className="rounded-[var(--radius-lg)] border border-dashed border-[var(--color-border)] bg-[var(--color-surface)] px-6 py-10 text-center">
            <p className="text-[var(--color-muted)]">No apps yet</p>
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

      <AlertSummary serverId={id} alerts={alerts} />
      <BackupStatusLine serverId={id} />

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete {server.name}?</DialogTitle>
          </DialogHeader>
          <p className="text-sm text-[var(--color-muted)]">
            This removes the server from your account. Type <strong className="text-[var(--color-ink)]">{server.name}</strong> to confirm.
          </p>
          <Input
            value={deleteConfirm}
            onChange={(e) => setDeleteConfirm(e.target.value)}
            placeholder={server.name}
          />
          <DialogFooter>
            <Button variant="secondary" onClick={() => setDeleteOpen(false)}>
              Cancel
            </Button>
            <Button
              variant="danger"
              disabled={deleteConfirm !== server.name}
              onClick={deleteServer}
            >
              Delete forever
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
    </FadeIn>
  );
}
