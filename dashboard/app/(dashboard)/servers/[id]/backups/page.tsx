"use client";

import { use, useState } from "react";
import { useServer } from "@/hooks/use-server";
import {
  useBackupHistory,
  useBackupSchedule,
  useBackupUsage,
  useTriggerBackup,
} from "@/hooks/use-backup";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { BackupHistory } from "@/components/dashboard/backup-history";
import { BackupScheduleComponent } from "@/components/dashboard/backup-schedule";
import { ArrowLeft, Server, HardDrive, RefreshCw } from "lucide-react";
import Link from "next/link";

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + " " + sizes[i];
}

export default function BackupsPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const { server, isLoading: serverLoading } = useServer(id);
  const { jobs, isLoading: historyLoading, refetch: refetchHistory } = useBackupHistory(id);
  const {
    schedule,
    isLoading: scheduleLoading,
    updateSchedule,
  } = useBackupSchedule(id);
  const { usage, isLoading: usageLoading, refetch: refetchUsage } = useBackupUsage(id);
  const { triggerBackup, isTriggering } = useTriggerBackup(id);

  const handleTrigger = async () => {
    const jobId = await triggerBackup();
    if (jobId) {
      // Poll for updates
      setTimeout(() => {
        refetchHistory();
        refetchUsage();
      }, 2000);
    }
  };

  const lastJob = jobs.find((j) => j.status === "success" || j.status === "partial");
  const lastBackupTime = lastJob?.started_at
    ? new Date(lastJob.started_at).toLocaleString()
    : null;
  const lastBackupSize = lastJob?.size_new_bytes
    ? formatBytes(lastJob.size_new_bytes)
    : null;

  return (
    <div className="space-y-6">
      <Link
        href={`/servers/${id}`}
        className="inline-flex items-center gap-2 text-sm text-gray-500 hover:text-gray-700"
      >
        <ArrowLeft className="h-4 w-4" />
        Back to server
      </Link>

      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <h1 className="text-2xl font-bold text-gray-900 dark:text-white">
            Backups
          </h1>
          {server && (
            <span className="text-sm text-muted-foreground">{server.name}</span>
          )}
        </div>
        <Button onClick={handleTrigger} disabled={isTriggering}>
          {isTriggering ? (
            <>
              <RefreshCw className="h-4 w-4 mr-2 animate-spin" />
              Starting...
            </>
          ) : (
            <>
              <RefreshCw className="h-4 w-4 mr-2" />
              Back Up Now
            </>
          )}
        </Button>
      </div>

      {/* Header */}
      <Card>
        <CardContent className="py-4">
          <div className="flex items-center gap-2 text-sm">
            {lastBackupTime ? (
              <>
                <span>Last backup: {lastBackupTime}</span>
                {lastBackupSize && (
                  <span className="text-muted-foreground">
                    ({lastBackupSize} added)
                  </span>
                )}
              </>
            ) : (
              <span className="text-muted-foreground">No backups yet</span>
            )}
          </div>
        </CardContent>
      </Card>

      {/* Storage Usage */}
      <Card>
        <CardHeader>
          <CardTitle className="text-lg flex items-center gap-2">
            <HardDrive className="h-5 w-5" />
            Storage Usage
          </CardTitle>
        </CardHeader>
        <CardContent>
          {usageLoading ? (
            <div className="text-sm text-muted-foreground">Loading...</div>
          ) : (
            <div className="text-sm">
              <span className="font-medium">
                {usage ? formatBytes(usage.total_bytes) : "0 B"}
              </span>{" "}
              used in backup storage
              {usage && usage.snapshot_count > 0 && (
                <span className="text-muted-foreground">
                  {" "}
                  ({usage.snapshot_count} snapshots)
                </span>
              )}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Schedule */}
      <Card>
        <CardHeader>
          <CardTitle className="text-lg">Schedule</CardTitle>
        </CardHeader>
        <CardContent>
          <BackupScheduleComponent
            schedule={schedule}
            onUpdate={updateSchedule}
            isLoading={scheduleLoading}
          />
        </CardContent>
      </Card>

      {/* Backup History */}
      <Card>
        <CardHeader>
          <CardTitle className="text-lg">Backup History</CardTitle>
        </CardHeader>
        <CardContent>
          {historyLoading ? (
            <div className="text-sm text-muted-foreground py-8 text-center">
              Loading backup history...
            </div>
          ) : (
            <BackupHistory jobs={jobs} />
          )}
        </CardContent>
      </Card>
    </div>
  );
}
