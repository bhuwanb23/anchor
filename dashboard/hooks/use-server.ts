"use client";

import { useState, useEffect, useCallback } from "react";
import api from "@/lib/api";
import type { Server, Deployment } from "@/types";

export function useServers() {
  const [servers, setServers] = useState<Server[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchServers = useCallback(async () => {
    try {
      setIsLoading(true);
      const res = await api.get<Server[]>("/api/v1/servers");
      setServers(res.data || []);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to fetch servers");
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchServers();
  }, [fetchServers]);

  return { servers, isLoading, error, refetch: fetchServers };
}

export function useServer(id: string) {
  const [server, setServer] = useState<Server | null>(null);
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    async function load() {
      try {
        setIsLoading(true);
        const serversRes = await api.get<Server[]>("/api/v1/servers");
        const found = serversRes.data?.find((s) => s.id === id) || null;
        setServer(found);
        setError(null);
      } catch (e) {
        setError(e instanceof Error ? e.message : "Failed to fetch server");
      } finally {
        setIsLoading(false);
      }
    }
    load();
  }, [id]);

  return { server, deployments, isLoading, error };
}
