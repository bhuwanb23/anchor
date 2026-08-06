"use client";

import { use, useEffect, useState } from "react";
import Link from "next/link";
import { ArrowLeft, RefreshCw } from "lucide-react";
import {
  useBackupHistory,
  useBackupSchedule,
  useBackupUsage,
  useTriggerBackup,
  useTriggerRestore,
} from "@/hooks/use-backup";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { BackupHistory } from "@/components/dashboard/backup-history";
import { BackupScheduleComponent } from "@/components/dashboard/backup-schedule";
import { RestoreDialog } from "@/components/dashboard/restore-dialog";
import { BackupsPageSkeleton, FadeIn, PageError } from "@/components/ui/page-states";
import { getWSClient, type WSMessage } from "@/lib/ws";
import type { BackupJob } from "@/types";

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(0))}${sizes[i]}`;
}

export default function BackupsPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const { jobs, isLoading, error, refetch: refetchHistory } = useBackupHistory(id, 4000);
  const { schedule, isLoading: scheduleLoading, updateSchedule } = useBackupSchedule(id);
  const { usage, isLoading: usageLoading, refetch: refetchUsage } = useBackupUsage(id);
  const { triggerBackup, isTriggering } = useTriggerBackup(id);
  const { triggerRestore } = useTriggerRestore(id);

  const [restoreJob, setRestoreJob] = useState<BackupJob | null>(null);
  const [activeCmdId, setActiveCmdId] = useState<string | null>(null);
  const [progressMsg, setProgressMsg] = useState<string | null>(null);
  const [editSchedule, setEditSchedule] = useState(false);
  const [showSkel, setShowSkel] = useState(false);

  // Skip skeleton flash if load is fast
  useEffect(() => {
    if (!isLoading) {
      setShowSkel(false);
      return;
    }
    const t = setTimeout(() => setShowSkel(true), 200);
    return () => clearTimeout(t);
  }, [isLoading]);

  useEffect(() => {
    if (!activeCmdId) return;
    const client = getWSClient();
    client.subscribeServer(id);
    const unsubP = client.on("command_progress", (msg: WSMessage) => {
      const p = msg.payload as { command_id?: string; message?: string };
      if (p?.command_id && p.command_id !== activeCmdId) return;
      if (p?.message) setProgressMsg(p.message);
      refetchHistory();
    });
    const unsubR = client.on("command_result", (msg: WSMessage) => {
      const p = msg.payload as { command_id?: string };
      if (p?.command_id && p.command_id !== activeCmdId) return;
      setActiveCmdId(null);
      setProgressMsg(null);
      refetchHistory();
      refetchUsage();
    });
    return () => {
      unsubP();
      unsubR();
    };
  }, [activeCmdId, id, refetchHistory, refetchUsage]);

  const handleTrigger = async () => {
    const jobId = await triggerBackup();
    if (jobId) {
      setActiveCmdId(jobId);
      setProgressMsg("Starting backup…");
      refetchHistory();
    }
  };

  if (error && !jobs.length && !isLoading) {
    return (
      <PageError
        message="We could not load your backup history. Try again in a moment."
        onRetry={() => refetchHistory()}
      />
    );
  }

  if (isLoading && showSkel && jobs.length === 0) {
    return <BackupsPageSkeleton />;
  }

  const used = usage?.total_bytes ?? 0;
  const limit = usage?.limit_bytes || 5 * 1024 * 1024 * 1024;
  const pct = usage?.percent_used ?? (limit ? (used / limit) * 100 : 0);
  const hour = schedule?.hour_utc ?? 2;

  return (
    <FadeIn>
      <div className="mx-auto max-w-3xl space-y-6">
        <Link
          href={`/servers/${id}`}
          className="inline-flex items-center gap-2 text-sm text-gray-500 hover:text-gray-700"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to server
        </Link>

        <div className="flex flex-wrap items-start justify-between gap-4">
          <div>
            <h1 className="text-2xl font-bold text-gray-900 dark:text-white">Backups</h1>
            <p className="mt-1 text-sm text-gray-500">
              {formatBytes(used)} used in backup storage
            </p>
            <p className="mt-1 text-sm text-gray-500">
              Schedule: Daily at {hour}:00 UTC{" "}
              <button
                type="button"
                className="text-blue-600 hover:underline"
                onClick={() => setEditSchedule((v) => !v)}
              >
                Edit
              </button>
            </p>
          </div>
          <Button onClick={handleTrigger} disabled={isTriggering || !!activeCmdId}>
            {isTriggering || activeCmdId ? (
              <>
                <RefreshCw className="mr-2 h-4 w-4 animate-spin" />
                Backing up…
              </>
            ) : (
              "Back Up Now"
            )}
          </Button>
        </div>

        {editSchedule && (
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Edit schedule</CardTitle>
            </CardHeader>
            <CardContent>
              <BackupScheduleComponent
                schedule={schedule}
                onUpdate={async (h) => {
                  const ok = await updateSchedule(h);
                  if (ok) setEditSchedule(false);
                  return ok;
                }}
                isLoading={scheduleLoading}
              />
            </CardContent>
          </Card>
        )}

        {(activeCmdId || jobs.some((j) => j.status === "running")) && (
          <Card className="border-blue-200 dark:border-blue-900">
            <CardContent className="py-4">
              <div className="flex items-center gap-2 text-sm font-medium text-blue-700 dark:text-blue-300">
                <RefreshCw className="h-4 w-4 animate-spin" />
                Backup in progress
              </div>
              <p className="mt-1 text-sm text-gray-600 dark:text-gray-300">
                {progressMsg || "Working through your projects…"}
              </p>
            </CardContent>
          </Card>
        )}

        <section>
          <h2 className="mb-3 text-lg font-semibold">History</h2>
          <BackupHistory
            jobs={jobs}
            onRestore={setRestoreJob}
            progressMessage={progressMsg}
          />
        </section>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Storage</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            {usageLoading && !usage ? (
              <div className="h-2 animate-pulse rounded bg-gray-200" />
            ) : (
              <>
                <p className="text-sm text-gray-600 dark:text-gray-300">
                  Using {formatBytes(used)} of your {formatBytes(limit)} included storage
                </p>
                <Progress value={Math.min(100, pct)} />
                {pct >= 80 && (
                  <Link href="/account" className="text-sm text-blue-600 hover:underline">
                    Upgrade storage
                  </Link>
                )}
              </>
            )}
          </CardContent>
        </Card>

        {restoreJob && (
          <RestoreDialog
            job={restoreJob}
            isOpen={!!restoreJob}
            onClose={() => setRestoreJob(null)}
            onRestore={async (snapshotId, projectName) => {
              const jobId = await triggerRestore({ snapshot_id: snapshotId, project_name: projectName });
              return jobId;
            }}
          />
        )}
      </div>
    </FadeIn>
  );
}
