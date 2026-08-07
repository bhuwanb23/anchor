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
      <Card className="cursor-pointer transition-[transform,box-shadow,border-color] duration-[var(--dur-med)] ease-[var(--ease-out)] hover:-translate-y-0.5 hover:border-[var(--color-accent)]/40 hover:shadow-[var(--shadow-lift)]">
        <CardHeader>
          <div className="flex items-center justify-between gap-2">
            <CardTitle className="truncate">{server.name || "Unnamed server"}</CardTitle>
            <StatusBadge status={server.status || "disconnected"} />
          </div>
        </CardHeader>
        <CardContent>
          <div className="space-y-1 text-sm text-[var(--color-muted)]">
            <p className="font-mono text-xs">ID: {idShort}</p>
            <p>Last seen: {lastSeen}</p>
          </div>
        </CardContent>
      </Card>
    </Link>
  );
}
