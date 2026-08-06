"use client";

import { useEffect, useRef, useState } from "react";
import { Check, Circle, Loader2 } from "lucide-react";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { getWSClient, type WSMessage } from "@/lib/ws";
import api from "@/lib/api";

const STEPS = [
  { key: "stopping", label: "Stopping current version" },
  { key: "restoring", label: "Restoring previous image" },
  { key: "starting", label: "Starting app" },
  { key: "health", label: "Health check" },
];

type Phase = "confirm" | "progress" | "success" | "failure";

interface RollbackDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  serverId: string;
  appId: string;
  image?: string;
  deploymentId?: string;
  onSuccess?: () => void;
}

export function RollbackDialog({
  open,
  onOpenChange,
  serverId,
  appId,
  image,
  deploymentId,
  onSuccess,
}: RollbackDialogProps) {
  const [phase, setPhase] = useState<Phase>("confirm");
  const [activeStep, setActiveStep] = useState(0);
  const [error, setError] = useState("");
  const commandId = useRef<string | null>(null);

  useEffect(() => {
    if (!open) {
      setPhase("confirm");
      setActiveStep(0);
      setError("");
      commandId.current = null;
    }
  }, [open]);

  useEffect(() => {
    if (!open || phase !== "progress" || !commandId.current) return;
    const client = getWSClient();
    client.subscribeServer(serverId);

    const onProgress = (msg: WSMessage) => {
      const p = msg.payload as { command_id?: string; message?: string; status?: string };
      if (p?.command_id && p.command_id !== commandId.current) return;
      const m = (p?.message || "").toLowerCase();
      if (m.includes("stop")) setActiveStep(0);
      else if (m.includes("restore") || m.includes("image") || m.includes("pull")) setActiveStep(1);
      else if (m.includes("start")) setActiveStep(2);
      else if (m.includes("health")) setActiveStep(3);
      else setActiveStep((s) => Math.min(s + 1, STEPS.length - 1));
    };

    const onResult = (msg: WSMessage) => {
      const p = msg.payload as { command_id?: string; status?: string; error?: string };
      if (p?.command_id && p.command_id !== commandId.current) return;
      if (p?.status === "success") {
        setActiveStep(STEPS.length - 1);
        setPhase("success");
        onSuccess?.();
      } else {
        setError(
          p?.error ||
            "Rollback failed. Your app may still be on the previous version — check status and try again."
        );
        setPhase("failure");
      }
    };

    const u1 = client.on("command_progress", onProgress);
    const u2 = client.on("command_result", onResult);
    return () => {
      u1();
      u2();
    };
  }, [open, phase, serverId, onSuccess]);

  const startRollback = async () => {
    setPhase("progress");
    setActiveStep(0);
    setError("");
    try {
      const body = deploymentId
        ? { target: "specific", deployment_id: deploymentId }
        : { target: "previous" };
      const res = await api.post<{ id?: string; command_id?: string }>(
        `/api/v1/servers/${serverId}/apps/${appId}/rollback`,
        body
      );
      commandId.current = res.data.command_id || res.data.id || `rollback-${Date.now()}`;
    } catch (e) {
      setError(
        e instanceof Error
          ? e.message
          : "We could not start the rollback. Check that the server is connected."
      );
      setPhase("failure");
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {phase === "confirm" && "Confirm rollback"}
            {phase === "progress" && "Rolling back…"}
            {phase === "success" && "Rollback complete"}
            {phase === "failure" && "Rollback failed"}
          </DialogTitle>
        </DialogHeader>

        {phase === "confirm" && (
          <div className="space-y-3 text-sm text-gray-600 dark:text-gray-300">
            <p>
              This will switch your app back
              {image ? (
                <>
                  {" "}
                  to <code className="rounded bg-gray-100 px-1 dark:bg-gray-800">{image}</code>
                </>
              ) : (
                " to an older version"
              )}
              .
            </p>
            <p>Your current version will be stopped. You can deploy again anytime.</p>
          </div>
        )}

        {phase === "progress" && (
          <ol className="space-y-2">
            {STEPS.map((s, i) => (
              <li key={s.key} className="flex items-center gap-2 text-sm">
                {i < activeStep ? (
                  <Check className="h-4 w-4 text-green-600" />
                ) : i === activeStep ? (
                  <Loader2 className="h-4 w-4 animate-spin text-blue-600" />
                ) : (
                  <Circle className="h-4 w-4 text-gray-300" />
                )}
                <span className={i === activeStep ? "font-medium" : ""}>{s.label}</span>
              </li>
            ))}
          </ol>
        )}

        {phase === "success" && (
          <p className="text-sm text-gray-600 dark:text-gray-300">
            Your app is running on the older version.
          </p>
        )}

        {phase === "failure" && (
          <p className="text-sm text-red-700 dark:text-red-300">{error}</p>
        )}

        <DialogFooter>
          {phase === "confirm" && (
            <>
              <Button variant="secondary" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
              <Button onClick={startRollback}>Confirm rollback</Button>
            </>
          )}
          {(phase === "success" || phase === "failure") && (
            <Button onClick={() => onOpenChange(false)}>Close</Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
