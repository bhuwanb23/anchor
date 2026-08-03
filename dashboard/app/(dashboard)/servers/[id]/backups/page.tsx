"use client";

import { use, useState } from "react";
import { useServer } from "@/hooks/use-server";
import {
  useBackupHistory,
  useBackupSchedule,
  useBackupUsage,
  useTriggerBackup,
  useTriggerRestore,
  useBackupVerification,
  useTriggerVerification,
} from "@/hooks/use-backup";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { BackupHistory } from "@/components/dashboard/backup-history";
import { BackupScheduleComponent } from "@/components/dashboard/backup-schedule";
import { RestoreDialog } from "@/components/dashboard/restore-dialog";
import { ArrowLeft, Server, HardDrive, RefreshCw, Shield } from "lucide-react";
import Link from "next/link";
import type { BackupJob } from "@/types";

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + " " + sizes[i];
}

function StorageHistoryChart({
  history,
  limitBytes,
}: {
  history: { size_bytes: number; recorded_at: string }[];
  limitBytes: number;
}) {
  const max = Math.max(limitBytes, ...history.map((h) => h.size_bytes), 1);
  const width = 320;
  const height = 64;
  const pad = 2;
  const points = history
    .map((h, i) => {
      const x =
        pad + (i / Math.max(1, history.length - 1)) * (width - pad * 2);
      const y =
        height - pad - (h.size_bytes / max) * (height - pad * 2);
      return `${x},${y}`;
    })
    .join(" ");

  return (
    <svg
      viewBox={`0 0 ${width} ${height}`}
      className="h-16 w-full text-emerald-600"
      preserveAspectRatio="none"
      role="img"
      aria-label="Backup storage usage over time"
    >
      <polyline
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        points={points}
      />
    </svg>
  );
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
  const { triggerRestore, isTriggering: isRestoring } = useTriggerRestore(id);
  const { verification, isLoading: verificationLoading, refetch: refetchVerification } = useBackupVerification(id);
  const { triggerVerification, isTriggering: isVerifying } = useTriggerVerification(id);

  const [restoreDialogOpen, setRestoreDialogOpen] = useState(false);
  const [selectedBackupJob, setSelectedBackupJob] = useState<BackupJob | null>(null);

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

  const handleRestoreClick = (job: BackupJob) => {
    setSelectedBackupJob(job);
    setRestoreDialogOpen(true);
  };

  const handleRestore = async (snapshotId: string, projectName: string) => {
    const jobId = await triggerRestore({ snapshot_id: snapshotId, project_name: projectName });
    if (jobId) {
      setTimeout(() => {
        refetchHistory();
      }, 2000);
    }
    return jobId;
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
            <div className="space-y-4">
              <div className="flex items-baseline justify-between gap-4 text-sm">
                <div>
                  <span className="font-medium">
                    {usage ? formatBytes(usage.total_bytes) : "0 B"}
                  </span>
                  <span className="text-muted-foreground">
                    {" "}
                    of {usage ? formatBytes(usage.limit_bytes || 1073741824) : "1 GB"} used
                  </span>
                  {usage && usage.snapshot_count > 0 && (
                    <span className="text-muted-foreground">
                      {" "}
                      ({usage.snapshot_count} snapshots)
                    </span>
                  )}
                </div>
                <span
                  className={
                    (usage?.percent_used ?? 0) >= 95
                      ? "font-medium text-red-600"
                      : (usage?.percent_used ?? 0) >= 80
                        ? "font-medium text-amber-600"
                        : "text-muted-foreground"
                  }
                >
                  {(usage?.percent_used ?? 0).toFixed(0)}%
                </span>
              </div>

              <div className="h-2 w-full overflow-hidden rounded-full bg-muted">
                <div
                  className={
                    (usage?.percent_used ?? 0) >= 95
                      ? "h-full rounded-full bg-red-500 transition-all"
                      : (usage?.percent_used ?? 0) >= 80
                        ? "h-full rounded-full bg-amber-500 transition-all"
                        : "h-full rounded-full bg-emerald-500 transition-all"
                  }
                  style={{
                    width: `${Math.min(100, usage?.percent_used ?? 0)}%`,
                  }}
                />
              </div>

              {usage?.history && usage.history.length > 1 && (
                <div>
                  <div className="mb-2 text-xs text-muted-foreground">
                    Usage over time
                  </div>
                  <StorageHistoryChart
                    history={usage.history}
                    limitBytes={usage.limit_bytes || 1073741824}
                  />
                </div>
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

      {/* Verification */}
      <Card>
        <CardHeader>
          <CardTitle className="text-lg flex items-center gap-2">
            <Shield className="h-5 w-5" />
            Verification
          </CardTitle>
        </CardHeader>
        <CardContent>
          {verificationLoading ? (
            <div className="text-sm text-muted-foreground">Loading...</div>
          ) : (
            <div className="space-y-3">
              <div className="text-sm">
                <span className="text-muted-foreground">Last verification: </span>
                {verification?.last_verification?.status ? (
                  <span className={verification.last_verification.status === "verified" ? "text-green-600" : "text-red-600"}>
                    {verification.last_verification.status === "verified" ? "Passed" : "Failed"}
                  </span>
                ) : (
                  <span className="text-muted-foreground">Never</span>
                )}
              </div>
              <div className="text-sm">
                <span className="text-muted-foreground">Weekly deep verification: </span>
                <span>{verification?.config?.verify_interval_hours ? `${verification.config.verify_interval_hours / 24} days` : "7 days"}</span>
              </div>
              <div className="text-sm">
                <span className="text-muted-foreground">Monthly full verification: </span>
                <span>{verification?.config?.full_verify_interval_hours ? `${verification.config.full_verify_interval_hours / 24} days` : "30 days"}</span>
              </div>
              <Button
                variant="secondary"
                size="sm"
                onClick={async () => {
                  await triggerVerification();
                  setTimeout(() => refetchVerification(), 2000);
                }}
                disabled={isVerifying}
              >
                {isVerifying ? (
                  <>
                    <RefreshCw className="h-4 w-4 mr-2 animate-spin" />
                    Verifying...
                  </>
                ) : (
                  <>
                    <Shield className="h-4 w-4 mr-2" />
                    Verify Now
                  </>
                )}
              </Button>
            </div>
          )}
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
            <BackupHistory jobs={jobs} onRestore={handleRestoreClick} />
          )}
        </CardContent>
      </Card>

      {/* Restore Dialog */}
      {selectedBackupJob && (
        <RestoreDialog
          job={selectedBackupJob}
          isOpen={restoreDialogOpen}
          onClose={() => {
            setRestoreDialogOpen(false);
            setSelectedBackupJob(null);
          }}
          onRestore={handleRestore}
        />
      )}
    </div>
  );
}
