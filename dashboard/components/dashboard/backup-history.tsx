"use client";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { BackupJob, BackupProjectResult } from "@/types";

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + " " + sizes[i];
}

function formatDate(dateStr: string): string {
  const date = new Date(dateStr);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffHours = diffMs / (1000 * 60 * 60);

  if (diffHours < 24) {
    return `Today, ${date.toLocaleTimeString("en-US", { hour: "numeric", minute: "2-digit" })}`;
  } else if (diffHours < 48) {
    return `Yesterday, ${date.toLocaleTimeString("en-US", { hour: "numeric", minute: "2-digit" })}`;
  }
  return date.toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

function formatDuration(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;
  const mins = Math.floor(seconds / 60);
  const secs = seconds % 60;
  return `${mins}m ${secs}s`;
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
}

export function BackupHistory({ jobs, onRestore }: BackupHistoryProps) {
  if (jobs.length === 0) {
    return (
      <div className="text-center py-8 text-muted-foreground">
        No backups yet
      </div>
    );
  }

  return (
    <div className="space-y-2">
      {jobs.map((job) => {
        const projects = parseProjectResults(job.project_results);
        const hasPartial = projects.some((p) => p.status === "partial");
        const hasFailed = projects.some((p) => p.status === "failed");

        return (
          <div
            key={job.id}
            className="flex items-center justify-between p-4 border rounded-lg"
          >
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-3">
                <div className="text-sm font-medium">
                  {job.started_at ? formatDate(job.started_at) : job.created_at}
                </div>
                <div className="text-sm text-muted-foreground">
                  {job.size_new_bytes
                    ? formatBytes(job.size_new_bytes)
                    : job.size_total_bytes
                      ? formatBytes(job.size_total_bytes)
                      : "—"}
                </div>
                {job.duration_seconds && (
                  <div className="text-xs text-muted-foreground">
                    {formatDuration(job.duration_seconds)}
                  </div>
                )}
              </div>
              <div className="flex items-center gap-2 mt-1">
                {projects.map((project) => (
                  <span key={project.name} className="text-xs">
                    <span className="text-muted-foreground">
                      {project.name}{" "}
                      {project.status === "success"
                        ? "✓"
                        : project.status === "failed"
                          ? "✗"
                          : "⚠"}
                    </span>
                    {project.error && (
                      <span className="text-destructive ml-1">
                        ({project.error})
                      </span>
                    )}
                  </span>
                ))}
              </div>
            </div>
            <div className="flex items-center gap-2">
              {job.status === "running" && (
                <Badge variant="info">Running</Badge>
              )}
              {job.status === "success" && !hasPartial && (
                <Badge variant="success">Success</Badge>
              )}
              {job.status === "partial" || hasPartial ? (
                <Badge variant="warning">Partial</Badge>
              ) : null}
              {(job.status === "failed" || hasFailed) && (
                <Badge variant="danger">Failed</Badge>
              )}
              {job.status === "pending" && (
                <Badge variant="default">Pending</Badge>
              )}
              {job.status === "success" && job.snapshot_id && onRestore && (
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => onRestore(job)}
                  className="ml-2"
                >
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
