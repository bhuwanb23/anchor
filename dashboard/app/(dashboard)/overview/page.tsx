"use client";

import { useServers } from "@/hooks/use-server";
import { useAuth } from "@/hooks/use-auth";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { ServerCard } from "@/components/dashboard/server-card";
import { Server } from "lucide-react";

export default function OverviewPage() {
  const { user } = useAuth();
  const { servers, isLoading, error } = useServers();

  const connectedCount = servers.filter((s) => s.status === "connected").length;

  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-2xl font-bold text-gray-900 dark:text-white">
          Welcome back{user?.email ? `, ${user.email.split("@")[0]}` : ""}
        </h1>
        <p className="text-gray-500 dark:text-gray-400">
          Here&apos;s an overview of your infrastructure.
        </p>
      </div>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium text-gray-500">Total Servers</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-3xl font-bold text-gray-900 dark:text-white">
              {isLoading ? "—" : servers.length}
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium text-gray-500">Connected</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-3xl font-bold text-green-600">
              {isLoading ? "—" : connectedCount}
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className="text-sm font-medium text-gray-500">Disconnected</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-3xl font-bold text-red-600">
              {isLoading ? "—" : servers.length - connectedCount}
            </div>
          </CardContent>
        </Card>
      </div>

      <div>
        <h2 className="mb-4 text-lg font-semibold text-gray-900 dark:text-white">Your Servers</h2>
        {error && <p className="text-sm text-red-500">{error}</p>}
        {isLoading ? (
          <div className="flex items-center gap-2 text-gray-500">
            <Server className="h-4 w-4 animate-pulse" />
            Loading servers...
          </div>
        ) : servers.length === 0 ? (
          <Card>
            <CardContent className="py-12 text-center text-gray-500">
              No servers yet. Add your first server to get started.
            </CardContent>
          </Card>
        ) : (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {servers.map((server) => (
              <ServerCard key={server.id} server={server} />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
