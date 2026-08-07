"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import api from "@/lib/api";
import type { BackupJob, BackupSchedule, BackupUsage, RestoreRequest, RestoreJob, VerificationSchedule, StorageStats } from "@/types";

export function useBackupHistory(serverId: string, pollIntervalMs = 10000) {
  const [jobs, setJobs] = useState<BackupJob[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const mountedRef = useRef(true);

  const fetchHistory = useCallback(async () => {
    if (!serverId) {
      setJobs([]);
      setIsLoading(false);
      return;
    }
    try {
      const res = await api.get<BackupJob[]>(
        `/api/v1/servers/${serverId}/backup/history`
      );
      if (mountedRef.current) {
        setJobs(res.data || []);
        setError(null);
      }
    } catch (e) {
      if (mountedRef.current) {
        setError(e instanceof Error ? e.message : "Failed to fetch backup history");
      }
    } finally {
      if (mountedRef.current) {
        setIsLoading(false);
      }
    }
  }, [serverId]);

  useEffect(() => {
    mountedRef.current = true;
    fetchHistory();
    return () => {
      mountedRef.current = false;
    };
  }, [fetchHistory]);

  useEffect(() => {
    const interval = setInterval(() => {
      fetchHistory();
    }, pollIntervalMs);
    return () => clearInterval(interval);
  }, [fetchHistory, pollIntervalMs]);

  return { jobs, isLoading, error, refetch: fetchHistory };
}

export function useBackupSchedule(serverId: string) {
  const [schedule, setSchedule] = useState<BackupSchedule | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const mountedRef = useRef(true);

  const fetchSchedule = useCallback(async () => {
    try {
      const res = await api.get<BackupSchedule>(
        `/api/v1/servers/${serverId}/backup/schedule`
      );
      if (mountedRef.current) {
        setSchedule(res.data);
        setError(null);
      }
    } catch (e) {
      if (mountedRef.current) {
        setError(e instanceof Error ? e.message : "Failed to fetch backup schedule");
      }
    } finally {
      if (mountedRef.current) {
        setIsLoading(false);
      }
    }
  }, [serverId]);

  useEffect(() => {
    mountedRef.current = true;
    fetchSchedule();
    return () => {
      mountedRef.current = false;
    };
  }, [fetchSchedule]);

  const updateSchedule = useCallback(
    async (hourUtc: number) => {
      try {
        await api.put(`/api/v1/servers/${serverId}/backup/schedule`, {
          hour_utc: hourUtc,
        });
        await fetchSchedule();
        return true;
      } catch (e) {
        setError(e instanceof Error ? e.message : "Failed to update schedule");
        return false;
      }
    },
    [serverId, fetchSchedule]
  );

  return { schedule, isLoading, error, updateSchedule, refetch: fetchSchedule };
}

export function useBackupUsage(serverId: string) {
  const [usage, setUsage] = useState<BackupUsage | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const mountedRef = useRef(true);

  const fetchUsage = useCallback(async () => {
    if (!serverId) {
      setUsage(null);
      setIsLoading(false);
      return;
    }
    try {
      const res = await api.get<BackupUsage>(
        `/api/v1/servers/${serverId}/backup/usage`
      );
      if (mountedRef.current) {
        setUsage(res.data);
        setError(null);
      }
    } catch (e) {
      if (mountedRef.current) {
        setError(e instanceof Error ? e.message : "Failed to fetch backup usage");
      }
    } finally {
      if (mountedRef.current) {
        setIsLoading(false);
      }
    }
  }, [serverId]);

  useEffect(() => {
    mountedRef.current = true;
    fetchUsage();
    return () => {
      mountedRef.current = false;
    };
  }, [fetchUsage]);

  return { usage, isLoading, error, refetch: fetchUsage };
}

export function useTriggerBackup(serverId: string) {
  const [isTriggering, setIsTriggering] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const triggerBackup = useCallback(async () => {
    setIsTriggering(true);
    setError(null);
    try {
      const res = await api.post<{ job_id: string; status: string }>(
        `/api/v1/servers/${serverId}/backup/trigger`,
        {}
      );
      setIsTriggering(false);
      return res.data.job_id;
    } catch (e) {
      setIsTriggering(false);
      setError(e instanceof Error ? e.message : "Failed to trigger backup");
      return null;
    }
  }, [serverId]);

  return { triggerBackup, isTriggering, error };
}

// ---------------------------------------------------------------------------
// Restore hooks
// ---------------------------------------------------------------------------

export function useRestoreHistory(serverId: string, pollIntervalMs = 10000) {
  const [jobs, setJobs] = useState<RestoreJob[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const mountedRef = useRef(true);

  const fetchHistory = useCallback(async () => {
    try {
      const res = await api.get<RestoreJob[]>(
        `/api/v1/servers/${serverId}/backup/restores`
      );
      if (mountedRef.current) {
        setJobs(res.data || []);
        setError(null);
      }
    } catch (e) {
      if (mountedRef.current) {
        setError(e instanceof Error ? e.message : "Failed to fetch restore history");
      }
    } finally {
      if (mountedRef.current) {
        setIsLoading(false);
      }
    }
  }, [serverId]);

  useEffect(() => {
    mountedRef.current = true;
    fetchHistory();
    return () => {
      mountedRef.current = false;
    };
  }, [fetchHistory]);

  useEffect(() => {
    const interval = setInterval(() => {
      fetchHistory();
    }, pollIntervalMs);
    return () => clearInterval(interval);
  }, [fetchHistory, pollIntervalMs]);

  return { jobs, isLoading, error, refetch: fetchHistory };
}

export function useTriggerRestore(serverId: string) {
  const [isTriggering, setIsTriggering] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const triggerRestore = useCallback(
    async (request: RestoreRequest) => {
      setIsTriggering(true);
      setError(null);
      try {
        const res = await api.post<{ job_id: string; status: string }>(
          `/api/v1/servers/${serverId}/backup/restore`,
          request
        );
        setIsTriggering(false);
        return res.data.job_id;
      } catch (e) {
        setIsTriggering(false);
        setError(e instanceof Error ? e.message : "Failed to trigger restore");
        return null;
      }
    },
    [serverId]
  );

  return { triggerRestore, isTriggering, error };
}

// ---------------------------------------------------------------------------
// Verification hooks
// ---------------------------------------------------------------------------

export function useBackupVerification(serverId: string) {
  const [verification, setVerification] = useState<VerificationSchedule | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const mountedRef = useRef(true);

  const fetchVerification = useCallback(async () => {
    try {
      const res = await api.get<VerificationSchedule>(
        `/api/v1/servers/${serverId}/backup/verification`
      );
      if (mountedRef.current) {
        setVerification(res.data);
        setError(null);
      }
    } catch (e) {
      if (mountedRef.current) {
        setError(e instanceof Error ? e.message : "Failed to fetch verification status");
      }
    } finally {
      if (mountedRef.current) {
        setIsLoading(false);
      }
    }
  }, [serverId]);

  useEffect(() => {
    mountedRef.current = true;
    fetchVerification();
    return () => {
      mountedRef.current = false;
    };
  }, [fetchVerification]);

  return { verification, isLoading, error, refetch: fetchVerification };
}

export function useTriggerVerification(serverId: string) {
  const [isTriggering, setIsTriggering] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const triggerVerification = useCallback(async () => {
    setIsTriggering(true);
    setError(null);
    try {
      await api.post(
        `/api/v1/servers/${serverId}/backup/verification/trigger`,
        {}
      );
      setIsTriggering(false);
      return true;
    } catch (e) {
      setIsTriggering(false);
      setError(e instanceof Error ? e.message : "Failed to trigger verification");
      return false;
    }
  }, [serverId]);

  return { triggerVerification, isTriggering, error };
}

// ---------------------------------------------------------------------------
// Storage stats hooks
// ---------------------------------------------------------------------------

export function useStorageStats(serverId: string) {
  const [stats, setStats] = useState<StorageStats | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const mountedRef = useRef(true);

  const fetchStats = useCallback(async () => {
    try {
      const res = await api.get<StorageStats>(
        `/api/v1/servers/${serverId}/backup/storage/stats`
      );
      if (mountedRef.current) {
        setStats(res.data);
        setError(null);
      }
    } catch (e) {
      if (mountedRef.current) {
        setError(e instanceof Error ? e.message : "Failed to fetch storage stats");
      }
    } finally {
      if (mountedRef.current) {
        setIsLoading(false);
      }
    }
  }, [serverId]);

  useEffect(() => {
    mountedRef.current = true;
    fetchStats();
    return () => {
      mountedRef.current = false;
    };
  }, [fetchStats]);

  return { stats, isLoading, error, refetch: fetchStats };
}

export function useTriggerMaintenance(serverId: string) {
  const [isTriggering, setIsTriggering] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const triggerMaintenance = useCallback(
    async (operation: string = "all") => {
      setIsTriggering(true);
      setError(null);
      try {
        await api.post(
          `/api/v1/servers/${serverId}/backup/storage/maintenance`,
          { operation }
        );
        setIsTriggering(false);
        return true;
      } catch (e) {
        setIsTriggering(false);
        setError(e instanceof Error ? e.message : "Failed to trigger maintenance");
        return false;
      }
    },
    [serverId]
  );

  return { triggerMaintenance, isTriggering, error };
}
