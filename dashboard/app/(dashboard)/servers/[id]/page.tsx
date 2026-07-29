"use client";

import { use } from "react";
import { useServer } from "@/hooks/use-server";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { StatusBadge } from "@/components/ui/badge";
import { LogViewer } from "@/components/dashboard/log-viewer";
import { ArrowLeft, Server } from "lucide-react";
import Link from "next/link";

export default function ServerDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const { server, isLoading, error } = useServer(id);

  if (isLoading) {
    return (
      <div className="flex items-center gap-2 text-gray-500">
        <Server className="h-4 w-4 animate-pulse" />
        Loading server...
      </div>
    );
  }

  if (error || !server) {
    return (
      <div className="space-y-4">
        <Link
          href="/servers"
          className="inline-flex items-center gap-2 text-sm text-gray-500 hover:text-gray-700"
        >
          <ArrowLeft className="h-4 w-4" />
          Back to servers
        </Link>
        <Card>
          <CardContent className="py-12 text-center text-red-500">
            {error || "Server not found"}
          </CardContent>
        </Card>
      </div>
    );
  }

  const lastSeen = new Date(server.last_seen).toLocaleString();
  const connectedAt = new Date(server.connected_at).toLocaleString();

  return (
    <div className="space-y-6">
      <Link
        href="/servers"
        className="inline-flex items-center gap-2 text-sm text-gray-500 hover:text-gray-700"
      >
        <ArrowLeft className="h-4 w-4" />
        Back to servers
      </Link>

      <div className="flex items-center gap-4">
        <h1 className="text-2xl font-bold text-gray-900 dark:text-white">{server.name}</h1>
        <StatusBadge status={server.status} />
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium text-gray-500">Server ID</CardTitle>
          </CardHeader>
          <CardContent>
            <code className="text-sm text-gray-900 dark:text-white">{server.id}</code>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium text-gray-500">Status</CardTitle>
          </CardHeader>
          <CardContent>
            <StatusBadge status={server.status} />
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium text-gray-500">Connected At</CardTitle>
          </CardHeader>
          <CardContent>
            <span className="text-sm text-gray-900 dark:text-white">{connectedAt}</span>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium text-gray-500">Last Seen</CardTitle>
          </CardHeader>
          <CardContent>
            <span className="text-sm text-gray-900 dark:text-white">{lastSeen}</span>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Logs</CardTitle>
        </CardHeader>
        <CardContent>
          <LogViewer logs={[]} />
        </CardContent>
      </Card>
    </div>
  );
}
