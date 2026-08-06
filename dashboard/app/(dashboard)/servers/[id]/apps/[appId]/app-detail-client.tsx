"use client";

import { useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import {
  ArrowLeft,
  ChevronDown,
  ExternalLink,
  MoreHorizontal,
  Rocket,
  RotateCcw,
  Square,
  Undo2,
} from "lucide-react";
import { useApp } from "@/hooks/use-app";
import { useServerStore } from "@/store/server-store";
import { StatusBadge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { EnvVarList } from "@/components/app/env-var-list";
import { DomainsSection } from "@/components/app/domains-section";
import { DeployDialog } from "@/components/app/deploy-dialog";
import { AppLogsPanel } from "@/components/logs/app-logs-panel";
import api from "@/lib/api";
import { toast } from "sonner";
import { getWSClient } from "@/lib/ws";

type Tab = "overview" | "logs" | "deployments" | "settings";

function timeAgo(iso?: string): string {
  if (!iso) return "";
  const ms = Date.now() - new Date(iso).getTime();
  const mins = Math.floor(ms / 60_000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 48) return `${hrs}h ago`;
  return `${Math.floor(hrs / 24)}d ago`;
}

function liveUrl(domain?: string): string | null {
  if (!domain) return null;
  return domain.startsWith("http") ? domain : `https://${domain}`;
}

export default function AppDetailClient({
  serverId,
  appId,
}: {
  serverId: string;
  appId: string;
}) {
  const searchParams = useSearchParams();
  const router = useRouter();
  const initialTab = (searchParams.get("tab") as Tab) || "overview";
  const [tab, setTab] = useState<Tab>(initialTab);
  const [deployOpen, setDeployOpen] = useState(searchParams.get("deploy") === "1");
  const [moreOpen, setMoreOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteConfirm, setDeleteConfirm] = useState("");
  const [mem, setMem] = useState(256);
  const [cpu, setCpu] = useState(50);
  const [port, setPort] = useState(80);
  const [savingSettings, setSavingSettings] = useState(false);

  const {
    app,
    envKeys,
    deployments,
    isLoading,
    error,
    refresh,
    refreshEnv,
    refreshDeployments,
    refreshApp,
  } = useApp(serverId, appId);

  const containers = useServerStore((s) => s.containers);
  const servers = useServerStore((s) => s.servers);
  const server = servers.find((s) => s.id === serverId);
  const projectContainers = useMemo(
    () => containers.filter((c) => c.project === app?.project_name),
    [containers, app?.project_name]
  );

  useEffect(() => {
    if (app) {
      setMem(app.memory_limit_mb || 256);
      setCpu(app.cpu_quota_percent || 50);
      setPort(app.app_port || 80);
    }
  }, [app]);

  useEffect(() => {
    if (!app?.project_name) return;
    const client = getWSClient();
    client.subscribeServer(serverId);
  }, [serverId, app?.project_name]);

  useEffect(() => {
    const t = searchParams.get("tab") as Tab | null;
    if (t) setTab(t);
    if (searchParams.get("deploy") === "1") setDeployOpen(true);
  }, [searchParams]);

  const setTabNav = (t: Tab) => {
    setTab(t);
    router.replace(`/servers/${serverId}/apps/${appId}?tab=${t}`, { scroll: false });
  };

  const lifecycle = async (action: "restart" | "stop" | "start") => {
    try {
      await api.post(`/api/v1/servers/${serverId}/apps/${appId}/${action}`);
      toast.success(`${action} requested`);
      refreshApp();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : `${action} failed`);
    }
  };

  const rollback = async (deploymentId?: string) => {
    try {
      const body = deploymentId
        ? { target: "specific", deployment_id: deploymentId }
        : { target: "previous" };
      await api.post(`/api/v1/servers/${serverId}/apps/${appId}/rollback`, body);
      toast.success("Rollback started");
      refreshDeployments();
      refreshApp();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Rollback failed");
    }
  };

  const saveSettings = async () => {
    setSavingSettings(true);
    try {
      await api.patch(`/api/v1/servers/${serverId}/apps/${appId}`, {
        memory_limit_mb: mem,
        cpu_quota_percent: cpu,
        app_port: port,
      });
      toast.success("Settings saved");
      refreshApp();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Save failed");
    } finally {
      setSavingSettings(false);
    }
  };

  const deleteApp = async () => {
    try {
      await api.delete(`/api/v1/servers/${serverId}/apps/${appId}`);
      toast.success("App deleted");
      router.push(`/servers/${serverId}`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Delete failed");
    }
  };

  if (isLoading) {
    return (
      <div className="flex items-center gap-2 text-gray-500">
        <div className="h-4 w-4 animate-spin rounded-full border-2 border-blue-600 border-t-transparent" />
        Loading app…
      </div>
    );
  }

  if (error || !app) {
    return (
      <div className="space-y-4">
        <Link href={`/servers/${serverId}`} className="text-sm text-gray-500 hover:underline">
          ← Back to server
        </Link>
        <p className="text-red-600">{error || "App not found"}</p>
      </div>
    );
  }

  const url = liveUrl(app.platform_domain);
  const currentDep = deployments[0];

  const tabs: { id: Tab; label: string }[] = [
    { id: "overview", label: "Overview" },
    { id: "logs", label: "Logs" },
    { id: "deployments", label: "Deployments" },
    { id: "settings", label: "Settings" },
  ];

  return (
    <div className="mx-auto max-w-5xl space-y-6">
      <Link
        href={`/servers/${serverId}`}
        className="inline-flex items-center gap-2 text-sm text-gray-500 hover:text-gray-700"
      >
        <ArrowLeft className="h-4 w-4" />
        Back to server
      </Link>

      {/* Header */}
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <div className="flex flex-wrap items-center gap-3">
            <h1 className="text-3xl font-bold text-gray-900 dark:text-white">
              {app.project_name}
            </h1>
            <StatusBadge status={app.status} />
          </div>
          {url && (
            <a
              href={url}
              target="_blank"
              rel="noopener noreferrer"
              className="mt-2 inline-flex items-center gap-1 text-sm text-blue-600 hover:underline"
            >
              {url.replace(/^https?:\/\//, "")}
              <ExternalLink className="h-3.5 w-3.5" />
            </a>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button onClick={() => setDeployOpen(true)}>
            <Rocket className="mr-1 h-4 w-4" />
            Deploy New Version
          </Button>
          <Button variant="secondary" onClick={() => rollback()}>
            <Undo2 className="mr-1 h-4 w-4" />
            Rollback
          </Button>
          <div className="relative">
            <Button variant="ghost" onClick={() => setMoreOpen((v) => !v)}>
              More <ChevronDown className="ml-1 h-4 w-4" />
            </Button>
            {moreOpen && (
              <div className="absolute right-0 z-20 mt-1 w-40 rounded-lg border bg-white py-1 shadow-lg dark:border-gray-700 dark:bg-gray-900">
                <button
                  type="button"
                  className="flex w-full items-center gap-2 px-3 py-2 text-sm hover:bg-gray-50 dark:hover:bg-gray-800"
                  onClick={() => {
                    setMoreOpen(false);
                    lifecycle("restart");
                  }}
                >
                  <RotateCcw className="h-3.5 w-3.5" /> Restart
                </button>
                <button
                  type="button"
                  className="flex w-full items-center gap-2 px-3 py-2 text-sm hover:bg-gray-50 dark:hover:bg-gray-800"
                  onClick={() => {
                    setMoreOpen(false);
                    lifecycle("stop");
                  }}
                >
                  <Square className="h-3.5 w-3.5" /> Stop
                </button>
                <button
                  type="button"
                  className="flex w-full items-center gap-2 px-3 py-2 text-sm text-red-600 hover:bg-red-50 dark:hover:bg-red-950/40"
                  onClick={() => {
                    setMoreOpen(false);
                    setDeleteOpen(true);
                  }}
                >
                  <MoreHorizontal className="h-3.5 w-3.5" /> Delete
                </button>
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Tabs */}
      <div className="border-b border-gray-200 dark:border-gray-800">
        <nav className="-mb-px flex gap-6">
          {tabs.map((t) => (
            <button
              key={t.id}
              type="button"
              onClick={() => setTabNav(t.id)}
              className={`border-b-2 px-1 pb-3 text-sm font-medium transition ${
                tab === t.id
                  ? "border-blue-600 text-blue-600"
                  : "border-transparent text-gray-500 hover:text-gray-800"
              }`}
            >
              {t.label}
            </button>
          ))}
        </nav>
      </div>

      {/* Overview */}
      {tab === "overview" && (
        <div className="space-y-8">
          <section>
            <h2 className="mb-3 text-lg font-semibold">Container Health</h2>
            {projectContainers.length === 0 ? (
              <p className="text-sm text-gray-500">
                Waiting for health reports… Containers appear here once the agent reports.
              </p>
            ) : (
              <div className="grid gap-3 sm:grid-cols-2">
                {projectContainers.map((c) => (
                  <Card key={`${c.project}-${c.role}`}>
                    <CardContent className="space-y-3 p-4">
                      <div className="flex items-center justify-between">
                        <span className="font-medium capitalize">{c.role || "app"}</span>
                        <StatusBadge status={c.status === "exited" ? "failed" : c.status} />
                      </div>
                      <div>
                        <div className="mb-1 flex justify-between text-xs text-gray-500">
                          <span>CPU</span>
                          <span>{(c.cpu_percent || 0).toFixed(1)}%</span>
                        </div>
                        <Progress value={Math.min(100, c.cpu_percent || 0)} />
                      </div>
                      <div>
                        <div className="mb-1 flex justify-between text-xs text-gray-500">
                          <span>RAM</span>
                          <span>
                            {c.ram_used_mb || 0}
                            {c.ram_limit_mb ? ` / ${c.ram_limit_mb}` : ""} MB
                          </span>
                        </div>
                        <Progress
                          value={
                            c.ram_limit_mb
                              ? Math.min(100, ((c.ram_used_mb || 0) / c.ram_limit_mb) * 100)
                              : 0
                          }
                        />
                      </div>
                      {(c.restart_count || 0) > 0 && (
                        <p className="text-xs font-medium text-orange-600">
                          Restarts: {c.restart_count}
                        </p>
                      )}
                    </CardContent>
                  </Card>
                ))}
              </div>
            )}
          </section>

          <section>
            <h2 className="mb-3 text-lg font-semibold">Environment Variables</h2>
            <EnvVarList
              serverId={serverId}
              appId={appId}
              keys={envKeys}
              onChange={refreshEnv}
              onRestart={() => lifecycle("restart")}
            />
          </section>

          <section>
            <h2 className="mb-3 text-lg font-semibold">Custom Domains</h2>
            <DomainsSection
              serverId={serverId}
              appId={appId}
              serverIp={server?.public_ip}
            />
          </section>
        </div>
      )}

      {/* Logs */}
      {tab === "logs" && (
        <div className="space-y-2">
          <div className="flex justify-end">
            <Link
              href={`/servers/${serverId}/apps/${appId}/logs`}
              className="text-xs text-blue-600 hover:underline"
            >
              Open full-page viewer
            </Link>
          </div>
          <div className="h-[28rem]">
            <AppLogsPanel
              serverId={serverId}
              projectName={app.project_name}
              fill
              enabled={tab === "logs"}
            />
          </div>
        </div>
      )}

      {/* Deployments */}
      {tab === "deployments" && (
        <div className="space-y-3">
          {deployments.length === 0 ? (
            <p className="text-sm text-gray-500">No deployments yet.</p>
          ) : (
            deployments.map((d, i) => {
              const isCurrent = i === 0 && (d.status === "running" || d.status === "success" || app.status === "running");
              const failed = d.status === "failed";
              return (
                <Card key={d.id}>
                  <CardContent className="flex flex-wrap items-center justify-between gap-3 p-4">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <code className="text-sm font-medium">{d.image}</code>
                        <StatusBadge status={failed ? "failed" : d.status} />
                        {isCurrent && (
                          <span className="rounded-full bg-blue-100 px-2 py-0.5 text-xs font-medium text-blue-700 dark:bg-blue-950 dark:text-blue-300">
                            Current
                          </span>
                        )}
                      </div>
                      <p
                        className="mt-1 text-xs text-gray-500"
                        title={d.created_at ? new Date(d.created_at).toLocaleString() : ""}
                      >
                        {timeAgo(d.created_at)}
                        {d.updated_at && d.created_at
                          ? ` · took ${Math.max(
                              1,
                              Math.round(
                                (new Date(d.updated_at).getTime() -
                                  new Date(d.created_at).getTime()) /
                                  1000
                              )
                            )}s`
                          : ""}
                      </p>
                    </div>
                    {!isCurrent && !failed && (
                      <Button size="sm" variant="secondary" onClick={() => rollback(d.id)}>
                        Rollback to this version
                      </Button>
                    )}
                  </CardContent>
                </Card>
              );
            })
          )}
        </div>
      )}

      {/* Settings */}
      {tab === "settings" && (
        <div className="space-y-8">
          <Card>
            <CardHeader>
              <CardTitle className="text-base">App configuration</CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <div>
                <label className="text-sm font-medium text-gray-700 dark:text-gray-300">
                  Memory limit: {mem} MB
                </label>
                <input
                  type="range"
                  min={64}
                  max={2048}
                  step={64}
                  value={mem}
                  onChange={(e) => setMem(Number(e.target.value))}
                  className="mt-2 w-full"
                />
              </div>
              <div>
                <label className="text-sm font-medium text-gray-700 dark:text-gray-300">
                  CPU quota: {cpu}%
                </label>
                <input
                  type="range"
                  min={10}
                  max={100}
                  step={5}
                  value={cpu}
                  onChange={(e) => setCpu(Number(e.target.value))}
                  className="mt-2 w-full"
                />
              </div>
              <Input
                label="App port"
                type="number"
                value={port}
                onChange={(e) => setPort(Number(e.target.value))}
              />
              <p className="text-xs text-gray-500">
                Changing resource limits does not require a redeploy — the agent updates limits in place.
              </p>
              <Button onClick={saveSettings} disabled={savingSettings}>
                {savingSettings ? "Saving…" : "Save Changes"}
              </Button>
            </CardContent>
          </Card>

          <div className="rounded-xl border border-red-200 bg-red-50 p-5 dark:border-red-900/50 dark:bg-red-950/30">
            <h3 className="font-semibold text-red-800 dark:text-red-200">Danger Zone</h3>
            <p className="mt-1 text-sm text-red-700 dark:text-red-300">
              Deleting this app is irreversible. Containers, env vars, and domains for this app will be removed.
            </p>
            <Button
              className="mt-4"
              variant="danger"
              onClick={() => {
                setDeleteConfirm("");
                setDeleteOpen(true);
              }}
            >
              Delete App
            </Button>
          </div>
        </div>
      )}

      <DeployDialog
        open={deployOpen}
        onOpenChange={setDeployOpen}
        serverId={serverId}
        appId={appId}
        projectName={app.project_name}
        currentImage={app.current_image || currentDep?.image}
        currentPort={app.app_port || currentDep?.port}
        liveUrl={url}
        onSuccess={() => {
          refresh();
        }}
      />

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete {app.project_name}?</DialogTitle>
          </DialogHeader>
          <p className="text-sm text-gray-600 dark:text-gray-300">
            This cannot be undone. Type <strong>{app.project_name}</strong> to confirm.
          </p>
          <Input
            value={deleteConfirm}
            onChange={(e) => setDeleteConfirm(e.target.value)}
            placeholder={app.project_name}
          />
          <DialogFooter>
            <Button variant="secondary" onClick={() => setDeleteOpen(false)}>
              Cancel
            </Button>
            <Button
              variant="danger"
              disabled={deleteConfirm !== app.project_name}
              onClick={deleteApp}
            >
              Delete forever
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
