"use client";

import { use } from "react";
import Link from "next/link";
import { ArrowLeft } from "lucide-react";
import { useAlerts } from "@/hooks/use-alerts";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

export default function ServerAlertsPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const { alerts, acknowledge } = useAlerts(id);
  const active = alerts.filter((a) => a.status === "active");

  return (
    <div className="mx-auto max-w-3xl space-y-6">
      <Link
        href={`/servers/${id}`}
        className="inline-flex items-center gap-2 text-sm text-gray-500 hover:text-gray-700"
      >
        <ArrowLeft className="h-4 w-4" />
        Back to server
      </Link>

      <Card>
        <CardHeader>
          <CardTitle>
            Alerts
            <span className="ml-2 text-sm font-normal text-gray-400">
              {active.length} active
            </span>
          </CardTitle>
        </CardHeader>
        <CardContent>
          {alerts.length === 0 ? (
            <p className="text-sm text-gray-500">No alerts — all systems normal.</p>
          ) : (
            <div className="space-y-3">
              {alerts.map((a) => (
                <div
                  key={a.id}
                  className={`rounded-r border-l-4 p-3 ${
                    a.status === "resolved"
                      ? "border-green-500 bg-green-50 dark:bg-green-950/40"
                      : a.severity === "critical"
                      ? "border-red-500 bg-red-50 dark:bg-red-950/40"
                      : "border-amber-500 bg-amber-50 dark:bg-amber-950/40"
                  }`}
                >
                  <div className="flex items-start justify-between gap-2">
                    <div>
                      <p className="text-sm font-medium text-gray-900 dark:text-white">
                        {a.title || a.message}
                      </p>
                      {a.title && a.message && a.title !== a.message && (
                        <p className="mt-1 text-sm text-gray-600 dark:text-gray-300">
                          {a.message}
                        </p>
                      )}
                      <p className="mt-1 text-xs text-gray-400">
                        {new Date(a.at).toLocaleString()}
                      </p>
                    </div>
                    {a.status === "active" && (
                      <button
                        type="button"
                        onClick={() => acknowledge(a.id)}
                        className="shrink-0 rounded-md border px-2 py-1 text-xs text-gray-600 hover:border-green-400 hover:text-green-600"
                      >
                        Acknowledge
                      </button>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
