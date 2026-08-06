"use client";

import { useState, useCallback } from "react";
import api from "@/lib/api";
import { toast } from "sonner";

interface CommandResult {
  command_id: string;
  status: "queued" | "in_progress" | "success" | "failed" | "timeout";
  result?: string;
}

export function useCommand() {
  const [pending, setPending] = useState<Record<string, CommandResult>>({});

  const trackCommand = useCallback((commandId: string) => {
    setPending((prev) => ({
      ...prev,
      [commandId]: { command_id: commandId, status: "queued" },
    }));
  }, []);

  const updateCommand = useCallback((commandId: string, status: CommandResult["status"], result?: string) => {
    setPending((prev) => ({
      ...prev,
      [commandId]: { command_id: commandId, status, result },
    }));
  }, []);

  const removeCommand = useCallback((commandId: string) => {
    setPending((prev) => {
      const next = { ...prev };
      delete next[commandId];
      return next;
    });
  }, []);

  const deploy = useCallback(async (serverId: string, appId: string, image: string, port: number) => {
    try {
      const res = await api.post(`/api/v1/servers/${serverId}/apps/${appId}/deploy`, { image, port });
      trackCommand(res.data.command_id);
      toast.success("Deploy started");
      return res.data;
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Deploy failed";
      toast.error(msg);
      throw err;
    }
  }, [trackCommand]);

  const rollback = useCallback(async (serverId: string, appId: string, deploymentId?: string) => {
    try {
      const body = deploymentId ? { target: "specific" as const, deployment_id: deploymentId } : {};
      const res = await api.post(`/api/v1/servers/${serverId}/apps/${appId}/rollback`, body);
      trackCommand(res.data.command_id);
      toast.success("Rollback started");
      return res.data;
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Rollback failed";
      toast.error(msg);
      throw err;
    }
  }, [trackCommand]);

  return { pending, trackCommand, updateCommand, removeCommand, deploy, rollback };
}
