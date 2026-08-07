"use client";

import Link from "next/link";
import { useBackupHistory } from "@/hooks/use-backup";
import type { BackupJob } from "@/types";

function formatBytes(n?: number): string {
  if (!n || n <= 0) return "";
  if (n < 1024) return `${n}B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)}KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(0)}MB`;
  return `${(n / (1024 * 1024 * 1024)).toFixed(1)}GB`;
}

function startOfDay(d: Date): Date {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate());
}

function formatBackupLine(job: BackupJob | undefined): { ok: boolean; text: string } {
  if (!job?.completed_at && !job?.started_at) {
    return { ok: false, text: "⚠ No backups yet" };
  }
  const when = new Date(job.completed_at || job.started_at || "");
  const now = new Date();
  const today = startOfDay(now);
  const yesterday = new Date(today);
  yesterday.setDate(yesterday.getDate() - 1);
  const day = startOfDay(when);
  const size = formatBytes(job.size_new_bytes || job.size_total_bytes);
  const time = when.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" }).toLowerCase();

  const ageDays = Math.floor((today.getTime() - day.getTime()) / 86_400_000);
  if (ageDays >= 2) {
    return {
      ok: false,
      text: `⚠ Backup overdue — last backup was ${ageDays} days ago`,
    };
  }
  if (day.getTime() === today.getTime()) {
    return {
      ok: true,
      text: `✓ Last backup: today at ${time}${size ? ` (${size})` : ""}`,
    };
  }
  if (day.getTime() === yesterday.getTime()) {
    return {
      ok: true,
      text: `✓ Last backup: yesterday at ${time}${size ? ` (${size})` : ""}`,
    };
  }
  return {
    ok: true,
    text: `✓ Last backup: ${when.toLocaleDateString()}${size ? ` (${size})` : ""}`,
  };
}

export function BackupStatusLine({ serverId }: { serverId: string }) {
  const { jobs } = useBackupHistory(serverId, 60_000);
  const lastSuccess = jobs.find((j) => j.status === "success" || j.status === "partial") || jobs[0];
  const line = formatBackupLine(lastSuccess);

  return (
    <div className="flex flex-wrap items-center justify-between gap-2 border-t border-[var(--color-border)] pt-4 text-sm border-[var(--color-border)]">
      <span className={line.ok ? "text-[var(--color-muted)]" : "text-amber-700 dark:text-amber-400"}>
        {line.text}
      </span>
      <Link
        href={`/servers/${serverId}/backups`}
        className="font-medium text-[var(--color-accent)] hover:underline "
      >
        Backups →
      </Link>
    </div>
  );
}
