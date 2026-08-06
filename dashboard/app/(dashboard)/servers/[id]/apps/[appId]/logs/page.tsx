"use client";

import { Suspense, use, useEffect, useState } from "react";
import Link from "next/link";
import { ArrowLeft } from "lucide-react";
import api from "@/lib/api";
import type { App } from "@/types";
import { AppLogsPanel } from "@/components/logs/app-logs-panel";

function LogsPageInner({
  serverId,
  appId,
}: {
  serverId: string;
  appId: string;
}) {
  const [projectName, setProjectName] = useState("");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api
      .get<App>(`/api/v1/servers/${serverId}/apps/${appId}`)
      .then((res) => setProjectName(res.data.project_name))
      .catch((e) => setError(e instanceof Error ? e.message : "Failed to load app"));
  }, [serverId, appId]);

  return (
    <div className="flex h-[calc(100vh-8rem)] min-h-[24rem] flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Link
          href={`/servers/${serverId}/apps/${appId}`}
          className="inline-flex items-center gap-2 text-sm text-gray-500 hover:text-gray-700"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to app
        </Link>
        {projectName && (
          <h1 className="text-lg font-semibold text-gray-900 dark:text-white">
            Logs · {projectName}
          </h1>
        )}
      </div>

      {error && <p className="text-sm text-red-600">{error}</p>}
      {projectName ? (
        <AppLogsPanel serverId={serverId} projectName={projectName} fill />
      ) : (
        !error && <p className="text-sm text-gray-500">Loading…</p>
      )}
    </div>
  );
}

export default function AppLogsPage({
  params,
}: {
  params: Promise<{ id: string; appId: string }>;
}) {
  const { id, appId } = use(params);
  return (
    <Suspense fallback={<p className="text-sm text-gray-500">Loading logs…</p>}>
      <LogsPageInner serverId={id} appId={appId} />
    </Suspense>
  );
}
