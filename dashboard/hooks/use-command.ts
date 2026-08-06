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
    } catch (err: any) {
      toast.error(err.response?.data?.message || "Deploy failed");
      throw err;
    }
  }, [trackCommand]);

  const rollback = useCallback(async (serverId: string, appId: string, deploymentId?: string) => {
    try {
      const body: any = deploymentId ? { target: "specific", deployment_id: deploymentId } : {};
      const res = await api.post(`/api/v1/servers/${serverId}/apps/${appId}/rollback`, body);
      trackCommand(res.data.command_id);
      toast.success("Rollback started");
      return res.data;
    } catch (err: any) {
      toast.error(err.response?.data?.message || "Rollback failed");
      throw err;
    }
  }, [trackCommand]);

  return { pending, trackCommand, updateCommand, removeCommand, deploy, rollback };
}
