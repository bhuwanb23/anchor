import Link from "next/link";
import { Card } from "@/components/ui/card";
import { StatusBadge } from "@/components/ui/badge";

interface AppCardProps {
  serverId: string;
  app: {
    id: string;
    project_name: string;
    status: string;
    current_image?: string;
  };
}

export function AppCard({ serverId, app }: AppCardProps) {
  return (
    <Link href={`/servers/${serverId}/apps/${app.id}`}>
      <Card className="cursor-pointer transition hover:shadow-md">
        <div className="p-4">
          <div className="flex items-center justify-between">
            <h3 className="font-medium">{app.project_name}</h3>
            <StatusBadge status={app.status} />
          </div>
          {app.current_image && (
            <p className="mt-1 truncate text-xs text-gray-500">{app.current_image}</p>
          )}
        </div>
      </Card>
    </Link>
  );
}
