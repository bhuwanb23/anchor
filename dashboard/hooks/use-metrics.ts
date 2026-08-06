"use client";

import { useState, useEffect, useRef } from "react";
import api from "@/lib/api";

interface Metrics {
  cpu_percent?: number;
  ram_used_mb?: number;
  ram_total_mb?: number;
  ram_percent?: number;
  disk_used_gb?: number;
  disk_total_gb?: number;
  disk_available_gb?: number;
  disk_used_percent?: number;
  load_1min?: number;
  docker_version?: string;
}

export function useMetrics(serverId: string | null, pollInterval = 10000) {
  const [metrics, setMetrics] = useState<Metrics | null>(null);
  const [loading, setLoading] = useState(true);
  const mountedRef = useRef(true);

  useEffect(() => {
    if (!serverId) return;
    mountedRef.current = true;

    const fetchMetrics = async () => {
      try {
        const res = await api.get(`/api/v1/servers/${serverId}`);
        if (mountedRef.current) {
          setMetrics(res.data.metrics || null);
          setLoading(false);
        }
      } catch {
        if (mountedRef.current) setLoading(false);
      }
    };

    fetchMetrics();
    const interval = setInterval(fetchMetrics, pollInterval);

    return () => {
      mountedRef.current = false;
      clearInterval(interval);
    };
  }, [serverId, pollInterval]);

  return { metrics, loading };
}
