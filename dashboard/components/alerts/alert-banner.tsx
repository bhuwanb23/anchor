"use client";

import { useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { X } from "lucide-react";
import type { Alert } from "@/types";

interface CriticalAlertBannerProps {
  serverId: string | null;
  alerts: Alert[];
}

/**
 * Session-dismissible banner for active critical alerts.
 * Reappears when a new critical alert arrives after dismissal.
 */
export function CriticalAlertBanner({ serverId, alerts }: CriticalAlertBannerProps) {
  const [dismissedIds, setDismissedIds] = useState<Set<string>>(new Set());

  const critical = useMemo(
    () =>
      alerts.filter(
        (a) =>
          a.status === "active" &&
          a.severity === "critical" &&
          !dismissedIds.has(a.id)
      ),
    [alerts, dismissedIds]
  );

  // Drop dismissals for alerts that are no longer active critical
  useEffect(() => {
    const activeIds = new Set(
      alerts.filter((a) => a.status === "active" && a.severity === "critical").map((a) => a.id)
    );
    setDismissedIds((prev) => {
      let changed = false;
      const next = new Set<string>();
      for (const id of prev) {
        if (activeIds.has(id)) next.add(id);
        else changed = true;
      }
      return changed ? next : prev;
    });
  }, [alerts]);

  if (!serverId || critical.length === 0) return null;

  const top = critical[0];
  const scope = top.project || "Server";
  const extra = critical.length > 1 ? ` (+${critical.length - 1} more)` : "";

  return (
    <div className="flex items-center gap-3 border-b border-[var(--color-danger)] bg-[var(--color-danger)] px-4 py-2.5 text-sm text-white">
      <p className="min-w-0 flex-1 truncate">
        <span className="font-semibold">{top.title || top.message}</span>
        <span className="opacity-90">
          {" "}
          · {scope}
          {extra}
        </span>
      </p>
      <Link
        href={`/servers/${serverId}/alerts`}
        className="shrink-0 font-semibold underline underline-offset-2 hover:no-underline"
      >
        View
      </Link>
      <button
        type="button"
        aria-label="Dismiss alert banner"
        className="shrink-0 rounded p-0.5 opacity-80 hover:bg-white/15 hover:opacity-100"
        onClick={() =>
          setDismissedIds((prev) => {
            const next = new Set(prev);
            for (const a of critical) next.add(a.id);
            return next;
          })
        }
      >
        <X className="h-4 w-4" />
      </button>
    </div>
  );
}
