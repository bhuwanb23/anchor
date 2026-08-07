"use client";

import { use, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { ArrowLeft } from "lucide-react";
import { useAlerts } from "@/hooks/use-alerts";
import { AlertCard, AlertsAllClear } from "@/components/alerts/alert-card";
import api from "@/lib/api";
import type { Alert, App } from "@/types";

type FilterTab = "active" | "resolved" | "all";

function severityRank(a: Alert): number {
  return a.severity === "critical" ? 0 : 1;
}

function sortAlerts(list: Alert[]): Alert[] {
  return [...list].sort((a, b) => {
    const sev = severityRank(a) - severityRank(b);
    if (sev !== 0) return sev;
    const at = new Date(a.fired_at || a.created_at || 0).getTime();
    const bt = new Date(b.fired_at || b.created_at || 0).getTime();
    return bt - at;
  });
}

function matchesFilter(a: Alert, tab: FilterTab): boolean {
  if (tab === "all") return true;
  if (tab === "resolved") return a.status === "resolved";
  return a.status === "active";
}

export default function ServerAlertsPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const { alerts, acknowledge } = useAlerts(id);
  const [tab, setTab] = useState<FilterTab>("active");
  const [apps, setApps] = useState<App[]>([]);

  useEffect(() => {
    api
      .get<App[] | { data?: App[] }>(`/api/v1/servers/${id}/apps`)
      .then((res) => {
        const list = Array.isArray(res.data)
          ? res.data
          : (res.data as { data?: App[] })?.data || [];
        setApps(list);
      })
      .catch(() => setApps([]));
  }, [id]);

  const projectToAppId = useMemo(() => {
    const m = new Map<string, string>();
    for (const a of apps) m.set(a.project_name, a.id);
    return m;
  }, [apps]);

  const filtered = useMemo(
    () => sortAlerts(alerts.filter((a) => matchesFilter(a, tab))),
    [alerts, tab]
  );

  const tabs: { id: FilterTab; label: string }[] = [
    { id: "active", label: "Active" },
    { id: "resolved", label: "Resolved" },
    { id: "all", label: "All" },
  ];

  return (
    <div className="mx-auto max-w-3xl space-y-6">
      <Link
        href={`/servers/${id}`}
        className="inline-flex items-center gap-2 text-sm text-[var(--color-muted)] hover:text-[var(--color-ink)]"
      >
        <ArrowLeft className="h-4 w-4" />
        Back to server
      </Link>

      <div>
        <p className="text-sm text-[var(--color-muted)]">
          Current and historical alerts for this server
        </p>
      </div>

      <div className="border-b border-[var(--color-border)]">
        <nav className="-mb-px flex gap-6">
          {tabs.map((t) => (
            <button
              key={t.id}
              type="button"
              onClick={() => setTab(t.id)}
              className={`border-b-2 px-1 pb-3 text-sm font-semibold transition ${
                tab === t.id
                  ? "border-[var(--color-accent)] text-[var(--color-accent)]"
                  : "border-transparent text-[var(--color-muted)] hover:text-[var(--color-ink)]"
              }`}
            >
              {t.label}
            </button>
          ))}
        </nav>
      </div>

      {tab === "active" && filtered.length === 0 ? (
        <AlertsAllClear />
      ) : filtered.length === 0 ? (
        <p className="text-sm text-[var(--color-muted)]">No alerts in this view.</p>
      ) : (
        <div className="space-y-3">
          {filtered.map((a) => {
            const appId = a.project ? projectToAppId.get(a.project) : undefined;
            const logsHref = appId
              ? `/servers/${id}/apps/${appId}/logs`
              : a.project
              ? `/servers/${id}`
              : null;
            return (
              <AlertCard
                key={a.id}
                alert={a}
                onAcknowledge={acknowledge}
                logsHref={logsHref}
              />
            );
          })}
        </div>
      )}
    </div>
  );
}
