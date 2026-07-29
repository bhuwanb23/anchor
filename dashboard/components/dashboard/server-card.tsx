"use client";

import Link from "next/link";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { StatusBadge } from "@/components/ui/badge";
import type { Server } from "@/types";

interface ServerCardProps {
  server: Server;
}

export function ServerCard({ server }: ServerCardProps) {
  const lastSeen = new Date(server.last_seen).toLocaleString();

  return (
    <Link href={`/servers/${server.id}`}>
      <Card className="cursor-pointer transition-colors hover:border-blue-300 hover:shadow-md dark:hover:border-blue-700">
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle>{server.name}</CardTitle>
            <StatusBadge status={server.status} />
          </div>
        </CardHeader>
        <CardContent>
          <div className="space-y-1 text-sm text-gray-500 dark:text-gray-400">
            <p>ID: {server.id.slice(0, 8)}...</p>
            <p>Last seen: {lastSeen}</p>
          </div>
        </CardContent>
      </Card>
    </Link>
  );
}
