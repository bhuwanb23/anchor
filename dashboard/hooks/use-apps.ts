"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import api from "@/lib/api";
import type { App } from "@/types";
import { useServerStore } from "@/store/server-store";
import type { AppCardModel } from "@/components/app/app-card";

interface ListResponse {
  data: App[];
  total: number;
}

export function useApps(serverId: string | null, pollIntervalMs = 15_000) {
  const [apps, setApps] = useState<AppCardModel[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const mountedRef = useRef(true);
  const containers = useServerStore((s) => s.containers);
  const alerts = useServerStore((s) => s.alerts);

  const fetchApps = useCallback(async () => {
    if (!serverId) return;
    try {
      const res = await api.get<ListResponse | App[]>(
        `/api/v1/servers/${serverId}/apps`
      );
      const raw = Array.isArray(res.data)
        ? res.data
        : (res.data as ListResponse).data || [];
      if (mountedRef.current) {
        setApps(raw);
        setError(null);
        setIsLoading(false);
      }
    } catch (e) {
      if (mountedRef.current) {
        setError(e instanceof Error ? e.message : "Failed to load apps");
        setIsLoading(false);
      }
    }
  }, [serverId]);

  useEffect(() => {
    mountedRef.current = true;
    setIsLoading(true);
    fetchApps();
    const t = setInterval(fetchApps, pollIntervalMs);
    return () => {
      mountedRef.current = false;
      clearInterval(t);
    };
  }, [fetchApps, pollIntervalMs]);

  const enriched: AppCardModel[] = apps.map((app) => {
    const c = containers.find(
      (x) => x.project === app.project_name && (x.role === "app" || !x.role)
    );
    let status = app.status;
    if (c) {
      if (c.status === "exited" || c.status === "dead") status = "failed";
      else if (c.status === "running") status = "running";
      else if (c.status === "restarting") status = "deploying";
    }
    const crashAlert = alerts.find(
      (a) =>
        a.status === "active" &&
        a.project === app.project_name &&
        (a.severity === "critical" ||
          (a.type || "").includes("crash") ||
          (a.type || "").includes("exit"))
    );
    return {
      ...app,
      status,
      crash_message: crashAlert?.message || crashAlert?.title,
      crashed_at: crashAlert?.fired_at,
    };
  });

  return { apps: enriched, isLoading, error, refetch: fetchApps };
}
