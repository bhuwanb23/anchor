import { StatusBadge } from "@/components/ui/badge";

interface DeploymentStatusProps {
  status: string;
  image?: string;
  createdAt?: string;
}

export function DeploymentStatus({ status, image, createdAt }: DeploymentStatusProps) {
  return (
    <div className="flex items-center gap-3">
      <StatusBadge status={status} />
      {image && <span className="text-sm text-gray-600 dark:text-gray-400">{image}</span>}
      {createdAt && <span className="text-xs text-gray-400">{new Date(createdAt).toLocaleString()}</span>}
    </div>
  );
}
