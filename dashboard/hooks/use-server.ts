"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import api from "@/lib/api";
import type { Server } from "@/types";
import { useServerStore } from "@/store/server-store";

export function useServers(pollIntervalMs = 5000) {
  const [servers, setServers] = useState<Server[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const mountedRef = useRef(true);

  const fetchServers = useCallback(async () => {
    try {
      const res = await api.get<Server[]>("/api/v1/servers");
      if (mountedRef.current) {
        setServers(res.data || []);
        setError(null);
      }
    } catch (e) {
      if (mountedRef.current) {
        setError(e instanceof Error ? e.message : "Failed to fetch servers");
      }
    } finally {
      if (mountedRef.current) {
        setIsLoading(false);
      }
    }
  }, []);

  useEffect(() => {
    mountedRef.current = true;
    fetchServers();
    return () => {
      mountedRef.current = false;
    };
  }, [fetchServers]);

  useEffect(() => {
    const interval = setInterval(() => {
      fetchServers();
    }, pollIntervalMs);
    return () => clearInterval(interval);
  }, [fetchServers, pollIntervalMs]);

  return { servers, isLoading, error, refetch: fetchServers };
}

export function useServer(id: string) {
  const [server, setServer] = useState<Server | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const storeServer = useServerStore((s) => s.servers.find((x) => x.id === id));
  const fetchServers = useServerStore((s) => s.fetchServers);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;

    async function load() {
      try {
        await fetchServers();
        const serversRes = await api.get<Server[]>("/api/v1/servers");
        if (mountedRef.current) {
          const found = serversRes.data?.find((s) => s.id === id) || null;
          setServer(found);
          setError(null);
        }
      } catch (e) {
        if (mountedRef.current) {
          setError(e instanceof Error ? e.message : "Failed to fetch server");
        }
      } finally {
        if (mountedRef.current) {
          setIsLoading(false);
        }
      }
    }
    load();

    return () => {
      mountedRef.current = false;
    };
  }, [id, fetchServers]);

  // Prefer live store status/version when available
  const merged =
    server && storeServer
      ? {
          ...server,
          ...storeServer,
          name: server.name || storeServer.name,
          status: storeServer.status || server.status,
          agent_version: storeServer.agent_version || server.agent_version,
        }
      : storeServer || server;

  return { server: merged, isLoading, error };
}
