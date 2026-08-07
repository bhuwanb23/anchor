"use client";

import Link from "next/link";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { StatusBadge } from "@/components/ui/badge";
import type { Server } from "@/types";

interface ServerCardProps {
  server: Server;
}

export function ServerCard({ server }: ServerCardProps) {
  const lastSeen = server.last_seen ? new Date(server.last_seen).toLocaleString() : "N/A";
  const idShort = server.id ? `${server.id.slice(0, 8)}…` : "—";

  return (
    <Link href={`/servers/${server.id}`}>
      <Card className="cursor-pointer transition-colors hover:border-blue-300 hover:shadow-md dark:hover:border-blue-700">
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle>{server.name || "Unnamed server"}</CardTitle>
            <StatusBadge status={server.status || "disconnected"} />
          </div>
        </CardHeader>
        <CardContent>
          <div className="space-y-1 text-sm text-gray-500 dark:text-gray-400">
            <p>ID: {idShort}</p>
            <p>Last seen: {lastSeen}</p>
          </div>
        </CardContent>
      </Card>
    </Link>
  );
}
