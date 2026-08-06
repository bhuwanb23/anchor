"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import { DeployDialog } from "@/components/app/deploy-dialog";
import { Rocket } from "lucide-react";

interface DeployButtonProps {
  serverId: string;
  appId: string;
  projectName: string;
  currentImage?: string;
  currentPort?: number;
  liveUrl?: string | null;
  onSuccess?: () => void;
}

export function DeployButton({
  serverId,
  appId,
  projectName,
  currentImage,
  currentPort,
  liveUrl,
  onSuccess,
}: DeployButtonProps) {
  const [open, setOpen] = useState(false);

  return (
    <>
      <Button size="sm" onClick={() => setOpen(true)}>
        <Rocket className="mr-1 h-3 w-3" />
        Deploy
      </Button>
      <DeployDialog
        open={open}
        onOpenChange={setOpen}
        serverId={serverId}
        appId={appId}
        projectName={projectName}
        currentImage={currentImage}
        currentPort={currentPort}
        liveUrl={liveUrl}
        onSuccess={onSuccess}
      />
    </>
  );
}
