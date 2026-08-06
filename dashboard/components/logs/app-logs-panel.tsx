"use client";

import { useState } from "react";
import { useLogStream } from "@/hooks/use-log-stream";
import { LogViewer, type ContainerRole } from "@/components/logs/log-viewer";

interface AppLogsPanelProps {
  serverId: string;
  projectName: string;
  fill?: boolean;
  className?: string;
  /** When false, panel is idle (e.g. inactive tab) */
  enabled?: boolean;
}

export function AppLogsPanel({
  serverId,
  projectName,
  fill = true,
  className,
  enabled = true,
}: AppLogsPanelProps) {
  const [container, setContainer] = useState<ContainerRole>("app");
  const { logs, status } = useLogStream({
    serverId,
    projectName,
    containers: [container],
    tail: 200,
    enabled: enabled && !!projectName,
    maxLines: 2000,
  });

  return (
    <LogViewer
      logs={logs}
      status={status}
      container={container}
      onContainerChange={setContainer}
      showContainerSelector
      fill={fill}
      className={className}
    />
  );
}
