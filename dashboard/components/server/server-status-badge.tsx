import { StatusBadge } from "@/components/ui/badge";

interface ServerStatusBadgeProps {
  status: string;
}

export function ServerStatusBadge({ status }: ServerStatusBadgeProps) {
  return <StatusBadge status={status} />;
}
