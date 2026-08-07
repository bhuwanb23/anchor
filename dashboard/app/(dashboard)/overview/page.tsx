"use client";

import { useEffect, useMemo, useState } from "react";
import Link from "next/link";
import {
  Plus,
  Server,
  Activity,
  Bell,
  HardDrive,
  Clock,
  ArrowRight,
  Wifi,
} from "lucide-react";
import { useServers } from "@/hooks/use-server";
import { useAuth } from "@/hooks/use-auth";
import { useApps } from "@/hooks/use-apps";
import { useAlerts } from "@/hooks/use-alerts";
import { useBackupUsage, useBackupHistory } from "@/hooks/use-backup";
import { useServerStore } from "@/store/server-store";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { ServerCard } from "@/components/dashboard/server-card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Progress } from "@/components/ui/progress";
import { PageError } from "@/components/ui/page-states";
import { Skeleton } from "@/components/ui/skeleton";

function formatAgo(iso?: string | null): string {
  if (!iso) return "—";
  const mins = Math.floor((Date.now() - new Date(iso).getTime()) / 60_000);
  if (Number.isNaN(mins)) return "—";
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 48) return `${hrs}h ago`;
  return `${Math.floor(hrs / 24)}d ago`;
}

function HealthBar({ label, pct }: { label: string; pct?: number | null }) {
  const ready = typeof pct === "number" && Number.isFinite(pct);
  const v = ready ? Math.min(100, Math.max(0, pct)) : 0;
  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between text-xs">
        <span className="font-medium text-[var(--color-muted)]">{label}</span>
        <span className="font-semibold tabular-nums text-[var(--color-ink)]">
          {ready ? `${Math.round(v)}%` : "Waiting"}
        </span>
      </div>
      <div className="h-2 overflow-hidden rounded-full bg-[var(--color-paper-2)]">
        <div
          className={`h-full rounded-full transition-[width] duration-500 ease-[var(--ease-out)] ${
            ready ? "bg-[var(--color-accent)]" : "bg-[var(--color-border)]"
          }`}
          style={{ width: ready ? `${v}%` : "12%" }}
        />
      </div>
    </div>
  );
}

export default function OverviewPage() {
  const { user } = useAuth();
  const { servers, isLoading, error, refetch } = useServers();
  const storeMetrics = useServerStore((s) => s.metrics);
  const [showSkel, setShowSkel] = useState(false);

  const connected = useMemo(
    () => servers.filter((s) => s.status === "connected"),
    [servers]
  );
  const focusServer = connected[0] || servers[0] || null;
  const focusId = focusServer?.id || null;

  const { apps } = useApps(focusId, 20_000);
  const { alerts } = useAlerts(focusId || "", !!focusId);
  const { usage } = useBackupUsage(focusId || "");
  const { jobs: backupJobs } = useBackupHistory(focusId || "", 30_000);

  useEffect(() => {
    if (!isLoading) {
      setShowSkel(false);
      return;
    }
    const t = setTimeout(() => setShowSkel(true), 200);
    return () => clearTimeout(t);
  }, [isLoading]);

  const connectedCount = connected.length;
  const appsRunning = apps.filter((a) => a.status === "running").length;
  const activeAlerts = alerts.filter((a) => a.status === "active");
  const greet = user?.name?.split(" ")[0] || user?.email?.split("@")[0];

  const metrics = focusServer?.metrics || storeMetrics;
  const lastBackup = backupJobs.find((j) => j.status === "success") || backupJobs[0];
  const storagePct =
    focusId && usage && typeof usage.percent_used === "number"
      ? usage.percent_used
      : null;

  return (
    <div className="mx-auto max-w-6xl space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <p className="text-sm font-medium text-[var(--color-muted)]">
            Welcome back{greet ? `, ${greet}` : ""}
          </p>
          <h2 className="mt-1 text-xl font-bold tracking-tight text-[var(--color-ink)] sm:text-2xl">
            Your infrastructure, calmly.
          </h2>
        </div>
        <Link href="/onboarding/connect-server">
          <Button size="sm" variant="secondary" className="gap-1.5">
            <Plus className="h-4 w-4" />
            Connect server
          </Button>
        </Link>
      </div>

      {error && !isLoading && (
        <PageError
          message="We could not load your servers. Try again in a moment."
          onRetry={() => refetch()}
        />
      )}

      {/* Featured + secondary stats */}
      <div className="grid gap-4 lg:grid-cols-12">
        <Card className="accent-gradient relative overflow-hidden border-0 lg:col-span-5">
          <div className="absolute -right-8 -top-8 h-32 w-32 rounded-full bg-white/10" />
          <CardContent className="relative space-y-4 py-2">
            <div className="flex items-center gap-2 text-sm font-semibold opacity-90">
              <Wifi className="h-4 w-4" />
              Connected now
            </div>
            <div className="text-5xl font-extrabold tracking-tight tabular-nums">
              {isLoading ? "—" : connectedCount}
            </div>
            <p className="max-w-xs text-sm opacity-85">
              {connectedCount === 0
                ? "No agents online yet. Connect a VPS to start seeing live health."
                : `${connectedCount} of ${servers.length} server${servers.length === 1 ? "" : "s"} reporting live.`}
            </p>
          </CardContent>
        </Card>

        <div className="grid gap-4 sm:grid-cols-3 lg:col-span-7">
          <Card>
            <CardContent className="space-y-2 py-1">
              <div className="flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-[var(--color-muted)]">
                <Server className="h-3.5 w-3.5" />
                Total
              </div>
              <p className="text-3xl font-extrabold tabular-nums text-[var(--color-ink)]">
                {isLoading ? "—" : servers.length}
              </p>
              <p className="text-xs text-[var(--color-muted)]">Registered servers</p>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="space-y-2 py-1">
              <div className="flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-[var(--color-muted)]">
                <Activity className="h-3.5 w-3.5" />
                Apps
              </div>
              <p className="text-3xl font-extrabold tabular-nums text-[var(--color-ink)]">
                {!focusId ? "—" : appsRunning}
              </p>
              <p className="text-xs text-[var(--color-muted)]">
                {focusServer ? `Running on ${focusServer.name}` : "Pick a server"}
              </p>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="space-y-2 py-1">
              <div className="flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-[var(--color-muted)]">
                <Bell className="h-3.5 w-3.5" />
                Alerts
              </div>
              <p className="text-3xl font-extrabold tabular-nums text-[var(--color-ink)]">
                {!focusId ? "—" : activeAlerts.length}
              </p>
              <p className="text-xs text-[var(--color-muted)]">
                Active on focus server
              </p>
            </CardContent>
          </Card>
        </div>
      </div>

      <div className="grid gap-4 lg:grid-cols-12">
        {/* Health strip */}
        <Card className="lg:col-span-7">
          <CardHeader className="mb-3 flex flex-row items-center justify-between">
            <CardTitle className="text-base">Server health</CardTitle>
            {focusServer && (
              <Badge variant="info">{focusServer.name}</Badge>
            )}
          </CardHeader>
          <CardContent className="space-y-4">
            {!focusServer ? (
              <p className="text-sm text-[var(--color-muted)]">
                Connect a server to see CPU, memory, and disk from the agent.
              </p>
            ) : !metrics ? (
              <p className="text-sm text-[var(--color-muted)]">
                Waiting for agent metrics…
              </p>
            ) : null}
            <HealthBar label="CPU" pct={metrics?.cpu_percent} />
            <HealthBar label="Memory" pct={metrics?.ram_percent} />
            <HealthBar label="Disk" pct={metrics?.disk_percent} />
            {servers.length > 1 && (
              <div className="flex flex-wrap gap-2 border-t border-[var(--color-border)] pt-4">
                {servers.slice(0, 6).map((s) => (
                  <Link
                    key={s.id}
                    href={`/servers/${s.id}`}
                    className="inline-flex items-center gap-1.5 rounded-full bg-[var(--color-paper-2)] px-2.5 py-1 text-xs font-semibold text-[var(--color-ink)] hover:bg-[var(--color-accent-soft)] hover:text-[var(--color-accent)]"
                  >
                    <span
                      className={`h-1.5 w-1.5 rounded-full ${
                        s.status === "connected"
                          ? "bg-[var(--color-success)]"
                          : "bg-[var(--color-muted)]"
                      }`}
                    />
                    {s.name}
                  </Link>
                ))}
              </div>
            )}
          </CardContent>
        </Card>

        {/* Reminders + uptime */}
        <div className="flex flex-col gap-4 lg:col-span-5">
          <Card>
            <CardHeader className="mb-2">
              <CardTitle className="flex items-center gap-2 text-base">
                <HardDrive className="h-4 w-4 text-[var(--color-accent)]" />
                Backups
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
              {!focusId ? (
                <p className="text-sm text-[var(--color-muted)]">
                  Connect a server to schedule backups.
                </p>
              ) : lastBackup ? (
                <p className="text-sm text-[var(--color-ink)]">
                  Latest backup{" "}
                  <span className="font-semibold">{lastBackup.status}</span>
                  {" · "}
                  {formatAgo(lastBackup.completed_at || lastBackup.started_at)}
                </p>
              ) : (
                <p className="text-sm text-[var(--color-muted)]">
                  No backups yet on this server.
                </p>
              )}
              {focusId && (
                <Link
                  href={`/servers/${focusId}/backups`}
                  className="inline-flex items-center gap-1 text-sm font-semibold text-[var(--color-accent)] hover:underline"
                >
                  Open backups <ArrowRight className="h-3.5 w-3.5" />
                </Link>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="mb-2">
              <CardTitle className="flex items-center gap-2 text-base">
                <Clock className="h-4 w-4 text-[var(--color-accent)]" />
                Agent pulse
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-2">
              {focusServer ? (
                <>
                  <p className="text-2xl font-extrabold tracking-tight text-[var(--color-ink)]">
                    {formatAgo(focusServer.last_seen || focusServer.connected_at)}
                  </p>
                  <p className="text-sm text-[var(--color-muted)]">
                    Last seen · {focusServer.name}
                    {focusServer.agent_version
                      ? ` · v${focusServer.agent_version}`
                      : ""}
                  </p>
                </>
              ) : (
                <p className="text-sm text-[var(--color-muted)]">
                  No agent connected yet.
                </p>
              )}
            </CardContent>
          </Card>
        </div>
      </div>

      <div className="grid gap-4 lg:grid-cols-12">
        {/* Recent alerts */}
        <Card className="lg:col-span-7">
          <CardHeader className="mb-3 flex flex-row items-center justify-between">
            <CardTitle className="text-base">Recent alerts</CardTitle>
            {focusId && (
              <Link
                href={`/servers/${focusId}/alerts`}
                className="text-sm font-semibold text-[var(--color-accent)] hover:underline"
              >
                View all
              </Link>
            )}
          </CardHeader>
          <CardContent>
            {!focusId ? (
              <p className="text-sm text-[var(--color-muted)]">
                Alerts will appear here once a server is connected.
              </p>
            ) : alerts.length === 0 ? (
              <p className="text-sm text-[var(--color-muted)]">
                All quiet — no alerts on this server.
              </p>
            ) : (
              <ul className="divide-y divide-[var(--color-border)]">
                {alerts.slice(0, 5).map((a) => (
                  <li key={a.id} className="flex items-start justify-between gap-3 py-3 first:pt-0 last:pb-0">
                    <div className="min-w-0">
                      <p className="truncate text-sm font-semibold text-[var(--color-ink)]">
                        {a.title || a.message}
                      </p>
                      <p className="mt-0.5 text-xs text-[var(--color-muted)]">
                        {formatAgo(a.fired_at || a.at)}
                        {a.project ? ` · ${a.project}` : ""}
                      </p>
                    </div>
                    <Badge
                      variant={
                        a.severity === "critical" || a.level === "critical"
                          ? "danger"
                          : a.status === "resolved"
                            ? "success"
                            : "warning"
                      }
                    >
                      {a.status === "active" ? a.severity || a.level : a.status}
                    </Badge>
                  </li>
                ))}
              </ul>
            )}
          </CardContent>
        </Card>

        {/* Storage gauge */}
        <Card className="lg:col-span-5">
          <CardHeader className="mb-3">
            <CardTitle className="text-base">Backup storage</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            {storagePct != null ? (
              <>
                <div className="flex items-end justify-between">
                  <p className="text-3xl font-extrabold tabular-nums text-[var(--color-ink)]">
                    {Math.round(storagePct)}%
                  </p>
                  <p className="text-xs text-[var(--color-muted)]">of plan limit</p>
                </div>
                <Progress value={storagePct} />
              </>
            ) : (
              <div className="rounded-[var(--radius-md)] bg-[var(--color-paper-2)] px-4 py-8 text-center">
                <p className="text-sm font-medium text-[var(--color-ink)]">
                  Storage usage unavailable
                </p>
                <p className="mt-1 text-xs text-[var(--color-muted)]">
                  Appears after backups start writing data.
                </p>
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Server grid */}
      <div>
        <div className="mb-4 flex items-center justify-between gap-2">
          <h3 className="text-lg font-bold tracking-tight text-[var(--color-ink)]">
            Your servers
          </h3>
        </div>

        {isLoading && showSkel ? (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {[1, 2, 3].map((i) => (
              <Skeleton key={i} className="h-36 rounded-[var(--radius-lg)]" />
            ))}
          </div>
        ) : isLoading ? null : !error && servers.length === 0 ? (
          <Card>
            <CardContent className="space-y-4 py-14 text-center">
              <div className="mx-auto flex h-14 w-14 items-center justify-center rounded-full bg-[var(--color-accent-soft)] text-[var(--color-accent)]">
                <Server className="h-6 w-6" />
              </div>
              <p className="text-lg font-bold text-[var(--color-ink)]">No servers yet</p>
              <p className="mx-auto max-w-sm text-sm text-[var(--color-muted)]">
                Connect a VPS when you&apos;re ready — or keep exploring the dashboard.
              </p>
              <Link href="/onboarding/connect-server">
                <Button className="gap-2">
                  <Plus className="h-4 w-4" />
                  Connect your first server
                </Button>
              </Link>
            </CardContent>
          </Card>
        ) : (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {servers.map((server) => (
              <ServerCard key={server.id} server={server} />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
