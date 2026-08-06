"use client";

import { RefreshCw } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { BackupJob, BackupProjectResult } from "@/types";

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(0))}${sizes[i]}`;
}

function formatRelative(dateStr: string): string {
  const date = new Date(dateStr);
  const now = new Date();
  const startToday = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const startThat = new Date(date.getFullYear(), date.getMonth(), date.getDate());
  const dayDiff = Math.round((startToday.getTime() - startThat.getTime()) / 86400000);
  const time = date.toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" });
  if (dayDiff === 0) return `Today at ${time}`;
  if (dayDiff === 1) return `Yesterday at ${time}`;
  return date.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

function parseProjectResults(projectResults?: string): BackupProjectResult[] {
  if (!projectResults) return [];
  try {
    return JSON.parse(projectResults);
  } catch {
    return [];
  }
}

interface BackupHistoryProps {
  jobs: BackupJob[];
  onRestore?: (job: BackupJob) => void;
  progressMessage?: string | null;
}

export function BackupHistory({ jobs, onRestore, progressMessage }: BackupHistoryProps) {
  if (jobs.length === 0) {
    return (
      <div className="rounded-xl border border-dashed border-gray-200 px-6 py-10 text-center dark:border-gray-800">
        <p className="text-gray-700 dark:text-gray-200">Your first backup will run tonight at 2am</p>
        <p className="mt-1 text-sm text-gray-500">You can also start one anytime with Back Up Now.</p>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      {jobs.map((job) => {
        const projects = parseProjectResults(job.project_results);
        const running = job.status === "running" || job.status === "pending";

        return (
          <div
            key={job.id}
            className="rounded-lg border p-4 dark:border-gray-800"
          >
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="min-w-0 space-y-1.5">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-sm font-medium">
                    {formatRelative(job.started_at || job.created_at)}
                  </span>
                  {running && (
                    <Badge variant="info" className="inline-flex items-center gap-1">
                      <RefreshCw className="h-3 w-3 animate-spin" /> Running
                    </Badge>
                  )}
                  {job.status === "success" && <Badge variant="success">Success</Badge>}
                  {job.status === "partial" && <Badge variant="warning">Partial</Badge>}
                  {job.status === "failed" && <Badge variant="danger">Failed</Badge>}
                  {job.verification_status === "verified" ? (
                    <span className="text-xs text-green-600">✓ Verified</span>
                  ) : job.status === "success" || job.status === "partial" ? (
                    <span className="text-xs text-amber-600">⚠ Unverified</span>
                  ) : null}
                </div>
                <div className="text-xs text-gray-500">
                  {job.size_new_bytes != null && (
                    <span>{formatBytes(job.size_new_bytes)} added</span>
                  )}
                  {job.size_total_bytes != null && (
                    <span>
                      {job.size_new_bytes != null ? " · " : ""}
                      {formatBytes(job.size_total_bytes)} total
                    </span>
                  )}
                </div>
                {running && progressMessage && (
                  <p className="text-xs text-blue-600">{progressMessage}</p>
                )}
                {projects.length > 0 && (
                  <div className="flex flex-wrap gap-2">
                    {projects.map((p) => (
                      <span
                        key={p.name}
                        className="text-xs text-gray-600 dark:text-gray-300"
                        title={p.error || undefined}
                      >
                        {p.name}{" "}
                        {p.status === "success" ? "✓" : p.status === "failed" ? "✗" : "⚠"}
                      </span>
                    ))}
                  </div>
                )}
              </div>
              {(job.status === "success" || job.status === "partial") &&
                job.snapshot_id &&
                onRestore && (
                  <Button variant="secondary" size="sm" onClick={() => onRestore(job)}>
                    Restore
                  </Button>
                )}
            </div>
          </div>
        );
      })}
    </div>
  );
}
