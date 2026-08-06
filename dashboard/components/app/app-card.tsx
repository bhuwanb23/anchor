"use client";

import { useState } from "react";
import Link from "next/link";
import { ExternalLink, MoreHorizontal, RotateCcw, Rocket, ScrollText } from "lucide-react";
import { StatusBadge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import api from "@/lib/api";
import { toast } from "sonner";
import type { App, AppStatus } from "@/types";

export interface AppCardModel extends App {
  crash_message?: string;
  crashed_at?: string;
  deploy_percent?: number;
  deploy_step?: string;
  deployed_at?: string;
}

interface AppCardProps {
  serverId: string;
  app: AppCardModel;
  onChanged?: () => void;
}

function displayStatus(status: string): string {
  if (status === "failed" || status === "crashed") return "Crashed";
  if (status === "deploying") return "Deploying";
  if (status === "running") return "Running";
  if (status === "stopped") return "Stopped";
  return status;
}

function timeAgo(iso?: string): string {
  if (!iso) return "";
  const ms = Date.now() - new Date(iso).getTime();
  const mins = Math.floor(ms / 60_000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins} minute${mins === 1 ? "" : "s"} ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 48) return `${hrs} hour${hrs === 1 ? "" : "s"} ago`;
  const days = Math.floor(hrs / 24);
  return `${days} day${days === 1 ? "" : "s"} ago`;
}

function liveUrl(app: App): string | null {
  if (app.platform_domain) {
    return app.platform_domain.startsWith("http")
      ? app.platform_domain
      : `https://${app.platform_domain}`;
  }
  return null;
}

export function AppCard({ serverId, app, onChanged }: AppCardProps) {
  const [busy, setBusy] = useState(false);
  const [menuOpen, setMenuOpen] = useState(false);
  const status = (app.status || "stopped") as AppStatus;
  const crashed = status === "failed" || status === "crashed";
  const deploying = status === "deploying";
  const url = liveUrl(app);

  const restart = async () => {
    setBusy(true);
    try {
      await api.post(`/api/v1/servers/${serverId}/apps/${app.id}/restart`);
      toast.success(`Restarting ${app.project_name}`);
      onChanged?.();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Restart failed");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div
      className={`rounded-xl border bg-white p-5 dark:bg-gray-900 ${
        crashed
          ? "border-l-4 border-l-red-500 border-gray-200 dark:border-gray-800"
          : "border-gray-200 dark:border-gray-800"
      }`}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <Link
            href={`/servers/${serverId}/apps/${app.id}`}
            className="text-lg font-semibold text-gray-900 hover:text-blue-600 dark:text-white"
          >
            {app.project_name}
          </Link>
          <div className="mt-1 flex flex-wrap items-center gap-2">
            <StatusBadge status={status === "crashed" ? "failed" : status} />
            <span className="text-xs text-gray-500">{displayStatus(status)}</span>
          </div>
          {crashed && (
            <p className="mt-1 text-sm text-red-600 dark:text-red-400">
              Crashed {timeAgo(app.crashed_at || app.updated_at) || "recently"}
            </p>
          )}
        </div>
      </div>

      {url && !deploying && (
        <a
          href={url}
          target="_blank"
          rel="noopener noreferrer"
          className="mt-3 inline-flex items-center gap-1 text-sm text-blue-600 hover:underline dark:text-blue-400"
        >
          {url.replace(/^https?:\/\//, "")}
          <ExternalLink className="h-3.5 w-3.5" />
        </a>
      )}

      {app.current_image && (
        <p className="mt-2 truncate text-xs text-gray-500">
          {app.current_image}
          {app.deployed_at || app.updated_at
            ? ` · deployed ${timeAgo(app.deployed_at || app.updated_at)}`
            : ""}
        </p>
      )}

      {crashed && (
        <p className="mt-3 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-800 dark:bg-red-950/40 dark:text-red-200">
          {app.crash_message ||
            `Your app ${app.project_name} stopped unexpectedly. Check logs for the reason.`}
        </p>
      )}

      {deploying && (
        <div className="mt-4 space-y-2">
          <Progress value={app.deploy_percent ?? 40} />
          <p className="text-sm text-gray-600 dark:text-gray-300">
            {app.deploy_step || "Starting your app..."}
          </p>
        </div>
      )}

      {!deploying && (
        <div className="mt-4 flex flex-wrap items-center gap-2">
          <Link href={`/servers/${serverId}/apps/${app.id}/logs`}>
            <Button
              size="sm"
              variant={crashed ? "primary" : "secondary"}
            >
              <ScrollText className="mr-1 h-3.5 w-3.5" />
              {crashed ? "View Logs" : "Logs"}
            </Button>
          </Link>
          <Button
            size="sm"
            variant={crashed ? "primary" : "secondary"}
            disabled={busy}
            onClick={restart}
          >
            <RotateCcw className="mr-1 h-3.5 w-3.5" />
            Restart
          </Button>
          <Link href={`/servers/${serverId}/apps/${app.id}?deploy=1`}>
            <Button size="sm" variant="secondary">
              <Rocket className="mr-1 h-3.5 w-3.5" />
              Deploy
            </Button>
          </Link>
          <div className="relative">
            <Button
              size="sm"
              variant="ghost"
              onClick={() => setMenuOpen((v) => !v)}
              aria-label="More actions"
            >
              <MoreHorizontal className="h-4 w-4" />
            </Button>
            {menuOpen && (
              <div className="absolute right-0 z-10 mt-1 w-36 rounded-lg border border-gray-200 bg-white py-1 shadow-lg dark:border-gray-700 dark:bg-gray-900">
                <Link
                  href={`/servers/${serverId}/apps/${app.id}`}
                  className="block px-3 py-2 text-sm text-gray-700 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-800"
                  onClick={() => setMenuOpen(false)}
                >
                  Open app
                </Link>
                <button
                  type="button"
                  className="block w-full px-3 py-2 text-left text-sm text-gray-700 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-800"
                  onClick={async () => {
                    setMenuOpen(false);
                    try {
                      await api.post(`/api/v1/servers/${serverId}/apps/${app.id}/stop`);
                      toast.success("Stop requested");
                      onChanged?.();
                    } catch {
                      toast.error("Stop failed");
                    }
                  }}
                >
                  Stop
                </button>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
