"use client";

import { CheckCircle2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { Alert } from "@/types";

function timeAgo(iso?: string | null): string {
  if (!iso) return "";
  const ms = Date.now() - new Date(iso).getTime();
  if (Number.isNaN(ms)) return "";
  const mins = Math.floor(ms / 60_000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins} minute${mins === 1 ? "" : "s"} ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 48) return `${hrs} hour${hrs === 1 ? "" : "s"} ago`;
  const days = Math.floor(hrs / 24);
  return `${days} day${days === 1 ? "" : "s"} ago`;
}

interface AlertCardProps {
  alert: Alert;
  onAcknowledge?: (id: string) => void;
  logsHref?: string | null;
}

export function AlertCard({ alert, onAcknowledge, logsHref }: AlertCardProps) {
  const severity = alert.severity === "critical" ? "critical" : "warning";
  const border =
    alert.status === "resolved"
      ? "border-l-[var(--color-success)]"
      : severity === "critical"
      ? "border-l-[var(--color-danger)]"
      : "border-l-[var(--color-warning)]";

  const scope = alert.project || "Server";

  return (
    <div
      className={`rounded-[var(--radius-md)] border border-[var(--color-border)] border-l-4 bg-[var(--color-surface)] p-4 shadow-[var(--shadow-card)] ${border}`}
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 space-y-1.5">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="font-semibold text-[var(--color-ink)]">
              {alert.title || alert.message}
            </h3>
            <span className="text-xs text-[var(--color-muted)]">{scope}</span>
          </div>
          <p className="text-xs text-[var(--color-muted)]" title={alert.fired_at}>
            Fired {timeAgo(alert.fired_at || alert.created_at)}
          </p>
          {!!alert.title && alert.message && alert.message !== alert.title && (
            <p className="text-sm text-[var(--color-ink)]">{alert.message}</p>
          )}
          {alert.action && (
            <p className="text-sm text-[var(--color-muted)]">
              <span className="font-medium text-[var(--color-ink)]">What to do: </span>
              {alert.action}
            </p>
          )}
          {alert.status === "resolved" && alert.resolved_at && (
            <p className="text-xs text-[var(--color-success)]">
              Resolved {timeAgo(alert.resolved_at)}
            </p>
          )}
          {alert.status === "acknowledged" && (
            <p className="text-xs text-[var(--color-muted)]">
              Acknowledged{alert.acknowledged_at ? ` ${timeAgo(alert.acknowledged_at)}` : ""}
            </p>
          )}
        </div>
        {alert.status === "active" && (
          <div className="flex shrink-0 flex-col gap-2">
            {logsHref && (
              <a href={logsHref}>
                <Button size="sm" variant="secondary">
                  View Logs
                </Button>
              </a>
            )}
            <Button size="sm" variant="secondary" onClick={() => onAcknowledge?.(alert.id)}>
              Acknowledge
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}

export function AlertsAllClear() {
  return (
    <div className="flex flex-col items-center justify-center gap-3 rounded-[var(--radius-lg)] border border-[var(--color-success)]/25 bg-[var(--color-success-soft)] px-6 py-12 text-center">
      <CheckCircle2 className="h-10 w-10 text-[var(--color-success)]" />
      <p className="text-base font-semibold text-[var(--color-success)]">
        All clear — no active alerts
      </p>
      <p className="max-w-sm text-sm text-[var(--color-muted)]">
        Your server is healthy. We&apos;ll notify you here if something needs attention.
      </p>
    </div>
  );
}
