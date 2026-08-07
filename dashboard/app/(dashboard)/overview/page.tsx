"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useServers } from "@/hooks/use-server";
import { useAuth } from "@/hooks/use-auth";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { ServerCard } from "@/components/dashboard/server-card";
import { Button } from "@/components/ui/button";
import { PageError } from "@/components/ui/page-states";
import { Plus } from "lucide-react";

export default function OverviewPage() {
  const { user } = useAuth();
  const { servers, isLoading, error, refetch } = useServers();
  const [showSkel, setShowSkel] = useState(false);

  useEffect(() => {
    if (!isLoading) {
      setShowSkel(false);
      return;
    }
    const t = setTimeout(() => setShowSkel(true), 200);
    return () => clearTimeout(t);
  }, [isLoading]);

  const connectedCount = servers.filter((s) => s.status === "connected").length;
  const greet = user?.name?.split(" ")[0] || user?.email?.split("@")[0];

  return (
    <div className="space-y-8">
      <div>
        <h1 className="text-2xl font-bold text-gray-900 dark:text-white">
          Welcome back{greet ? `, ${greet}` : ""}
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
            <div className="text-3xl font-bold text-gray-500">
              {isLoading ? "—" : Math.max(0, servers.length - connectedCount)}
            </div>
          </CardContent>
        </Card>
      </div>

      <div>
        <div className="mb-4 flex flex-wrap items-center justify-between gap-2">
          <h2 className="text-lg font-semibold text-gray-900 dark:text-white">Your Servers</h2>
          <Link href="/onboarding/connect-server">
            <Button size="sm" variant="secondary">
              <Plus className="mr-1 h-4 w-4" />
              Connect server
            </Button>
          </Link>
        </div>

        {error && !isLoading && (
          <PageError
            message="We could not load your servers. Try again in a moment."
            onRetry={() => refetch()}
          />
        )}

        {isLoading && showSkel ? (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {[1, 2, 3].map((i) => (
              <div key={i} className="h-36 animate-pulse rounded-xl bg-gray-200 dark:bg-gray-800" />
            ))}
          </div>
        ) : isLoading ? null : !error && servers.length === 0 ? (
          <Card>
            <CardContent className="space-y-3 py-12 text-center">
              <p className="text-lg font-medium text-gray-900 dark:text-white">No servers yet</p>
              <p className="text-sm text-gray-500">
                Connect a VPS when you&apos;re ready — or keep exploring the dashboard.
              </p>
              <Link href="/onboarding/connect-server">
                <Button>
                  <Plus className="mr-2 h-4 w-4" />
                  Connect your first server
                </Button>
              </Link>
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
