"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import api from "@/lib/api";
import type { Server } from "@/types";

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
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;

    async function load() {
      try {
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
  }, [id]);

  return { server, isLoading, error };
}
