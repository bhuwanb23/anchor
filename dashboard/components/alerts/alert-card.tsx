"use client";

import { Button } from "@/components/ui/button";
import { StatusBadge } from "@/components/ui/badge";
import api from "@/lib/api";
import { toast } from "sonner";

interface AlertCardProps {
  alert: {
    id: string;
    server_id: string;
    severity: string;
    title: string;
    message?: string;
    acknowledged: boolean;
    created_at: string;
  };
  onAck?: () => void;
}

export function AlertCard({ alert, onAck }: AlertCardProps) {
  const handleAck = async () => {
    try {
      await api.post(`/api/v1/servers/${alert.server_id}/alerts/${alert.id}/ack`);
      toast.success("Alert acknowledged");
      onAck?.();
    } catch {
      toast.error("Failed to acknowledge alert");
    }
  };

  return (
    <div className="flex items-start justify-between rounded-lg border p-4 dark:border-gray-800">
      <div className="space-y-1">
        <div className="flex items-center gap-2">
          <StatusBadge status={alert.severity} />
          <span className="font-medium">{alert.title}</span>
        </div>
        {alert.message && <p className="text-sm text-gray-500">{alert.message}</p>}
        <p className="text-xs text-gray-400">{new Date(alert.created_at).toLocaleString()}</p>
      </div>
      {!alert.acknowledged && (
        <Button size="sm" variant="ghost" onClick={handleAck}>
          Ack
        </Button>
      )}
    </div>
  );
}
