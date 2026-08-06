"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import api from "@/lib/api";
import type { App, Deployment } from "@/types";
import type { EnvKey } from "@/components/app/env-var-list";

interface ListResponse<T> {
  data: T[];
  total?: number;
}

export function useApp(serverId: string, appId: string) {
  const [app, setApp] = useState<App | null>(null);
  const [envKeys, setEnvKeys] = useState<EnvKey[]>([]);
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const mounted = useRef(true);

  const refreshApp = useCallback(async () => {
    const res = await api.get<App>(`/api/v1/servers/${serverId}/apps/${appId}`);
    if (mounted.current) setApp(res.data);
  }, [serverId, appId]);

  const refreshEnv = useCallback(async () => {
    try {
      const res = await api.get<{ keys: EnvKey[] }>(
        `/api/v1/servers/${serverId}/apps/${appId}/env`
      );
      if (mounted.current) setEnvKeys(res.data.keys || []);
    } catch {
      if (mounted.current) setEnvKeys([]);
    }
  }, [serverId, appId]);

  const refreshDeployments = useCallback(async () => {
    try {
      const res = await api.get<ListResponse<Deployment> | Deployment[]>(
        `/api/v1/servers/${serverId}/apps/${appId}/deployments`
      );
      const list = Array.isArray(res.data)
        ? res.data
        : (res.data as ListResponse<Deployment>).data || [];
      if (mounted.current) setDeployments(list);
    } catch {
      if (mounted.current) setDeployments([]);
    }
  }, [serverId, appId]);

  const refresh = useCallback(async () => {
    try {
      await Promise.all([refreshApp(), refreshEnv(), refreshDeployments()]);
      if (mounted.current) setError(null);
    } catch (e) {
      if (mounted.current) {
        setError(e instanceof Error ? e.message : "Failed to load app");
      }
    } finally {
      if (mounted.current) setIsLoading(false);
    }
  }, [refreshApp, refreshEnv, refreshDeployments]);

  useEffect(() => {
    mounted.current = true;
    setIsLoading(true);
    refresh();
    const t = setInterval(refreshApp, 15_000);
    return () => {
      mounted.current = false;
      clearInterval(t);
    };
  }, [refresh, refreshApp]);

  return {
    app,
    envKeys,
    deployments,
    isLoading,
    error,
    refresh,
    refreshEnv,
    refreshDeployments,
    refreshApp,
  };
}
