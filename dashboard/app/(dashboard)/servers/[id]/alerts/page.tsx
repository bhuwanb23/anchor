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
        className="inline-flex items-center gap-2 text-sm text-gray-500 hover:text-gray-700"
      >
        <ArrowLeft className="h-4 w-4" />
        Back to server
      </Link>

      <div>
        <h1 className="text-2xl font-bold text-gray-900 dark:text-white">Alerts</h1>
        <p className="mt-1 text-sm text-gray-500">
          Current and historical alerts for this server
        </p>
      </div>

      <div className="border-b border-gray-200 dark:border-gray-800">
        <nav className="-mb-px flex gap-6">
          {tabs.map((t) => (
            <button
              key={t.id}
              type="button"
              onClick={() => setTab(t.id)}
              className={`border-b-2 px-1 pb-3 text-sm font-medium transition ${
                tab === t.id
                  ? "border-blue-600 text-blue-600"
                  : "border-transparent text-gray-500 hover:text-gray-800"
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
        <p className="text-sm text-gray-500">No alerts in this view.</p>
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
