"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { Check, Circle, Loader2, ExternalLink } from "lucide-react";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Progress } from "@/components/ui/progress";
import { getWSClient, type WSMessage } from "@/lib/ws";
import api from "@/lib/api";

type Phase = "input" | "progress" | "success" | "failure";

const DEPLOY_STEPS = [
  { key: "pulling", label: "Pulling image" },
  { key: "starting", label: "Starting your app" },
  { key: "health", label: "Waiting for health check" },
  { key: "routing", label: "Configuring HTTPS" },
  { key: "complete", label: "Finishing up" },
];

interface DeployDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  serverId: string;
  appId: string;
  projectName: string;
  currentImage?: string;
  currentPort?: number;
  liveUrl?: string | null;
  onSuccess?: () => void;
}

function stepIndex(phase: string): number {
  const i = DEPLOY_STEPS.findIndex((s) => s.key === phase || phase.includes(s.key));
  return i >= 0 ? i : 0;
}

export function DeployDialog({
  open,
  onOpenChange,
  serverId,
  appId,
  projectName,
  currentImage,
  currentPort = 80,
  liveUrl,
  onSuccess,
}: DeployDialogProps) {
  const [phase, setPhase] = useState<Phase>("input");
  const [image, setImage] = useState(currentImage || "");
  const [port, setPort] = useState(currentPort);
  const [memory, setMemory] = useState(0);
  const [advanced, setAdvanced] = useState(false);
  const [percent, setPercent] = useState(0);
  const [activeStep, setActiveStep] = useState(0);
  const [completedSteps, setCompletedSteps] = useState<Set<number>>(new Set());
  const [durationSec, setDurationSec] = useState(0);
  const [errorLogs, setErrorLogs] = useState("");
  const [resultUrl, setResultUrl] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const startedAt = useRef<number>(0);
  const commandId = useRef<string | null>(null);

  useEffect(() => {
    if (open && phase === "input") {
      setImage(currentImage || "");
      setPort(currentPort || 80);
    }
  }, [open, currentImage, currentPort, phase]);

  const resetToInput = () => {
    setPhase("input");
    setPercent(0);
    setActiveStep(0);
    setCompletedSteps(new Set());
    setErrorLogs("");
    setDurationSec(0);
    commandId.current = null;
  };

  const handleClose = (next: boolean) => {
    if (!next && phase === "progress") {
      // Allow close during progress — deploy continues on server
      onOpenChange(false);
      return;
    }
    if (!next) {
      resetToInput();
    }
    onOpenChange(next);
  };

  useEffect(() => {
    if (!open || phase !== "progress" || !commandId.current) return;
    const client = getWSClient();
    client.subscribeServer(serverId);

    const onProgress = (msg: WSMessage) => {
      const p = (msg.payload || {}) as {
        command_id?: string;
        phase?: string;
        message?: string;
        percent?: number;
        status?: string;
      };
      if (p.command_id && commandId.current && p.command_id !== commandId.current) return;
      if (typeof p.percent === "number") setPercent(p.percent);
      const idx = stepIndex(p.phase || p.message || "");
      setActiveStep(idx);
      setCompletedSteps((prev) => {
        const next = new Set(prev);
        for (let i = 0; i < idx; i++) next.add(i);
        return next;
      });
    };

    const onResult = (msg: WSMessage) => {
      const p = (msg.payload || msg) as {
        command_id?: string;
        status?: string;
        output?: string;
        result?: string;
        error?: string;
        logs?: string;
      };
      if (p.command_id && commandId.current && p.command_id !== commandId.current) return;
      const elapsed = Math.max(1, Math.round((Date.now() - startedAt.current) / 1000));
      setDurationSec(elapsed);
      const ok = p.status === "success";
      if (ok) {
        setPercent(100);
        setCompletedSteps(new Set(DEPLOY_STEPS.map((_, i) => i)));
        setResultUrl(liveUrl || null);
        setPhase("success");
        onSuccess?.();
      } else {
        const logs = p.logs || p.error || p.output || p.result || "Deploy failed";
        setErrorLogs(String(logs).split("\n").slice(-20).join("\n"));
        setPhase("failure");
      }
    };

    const u1 = client.on("command_progress", onProgress);
    const u2 = client.on("command_result", onResult);
    const u3 = client.on("result", onResult);
    return () => {
      u1();
      u2();
      u3();
    };
  }, [open, phase, serverId, liveUrl, onSuccess]);

  const startDeploy = async (e: React.FormEvent) => {
    e.preventDefault();
    setSubmitting(true);
    try {
      const body: Record<string, unknown> = { image, port };
      if (advanced && memory > 0) body.memory_limit_mb = memory;
      const res = await api.post<{ command_id: string; deployment_id?: string }>(
        `/api/v1/servers/${serverId}/apps/${appId}/deploy`,
        body
      );
      commandId.current = res.data.command_id;
      startedAt.current = Date.now();
      setPhase("progress");
      setPercent(5);
      setActiveStep(0);
      setCompletedSteps(new Set());
    } catch (err) {
      setErrorLogs(err instanceof Error ? err.message : "Failed to start deploy");
      setPhase("failure");
    } finally {
      setSubmitting(false);
    }
  };

  const title =
    phase === "input"
      ? `Deploy ${projectName}`
      : phase === "progress"
      ? `Deploying ${projectName}...`
      : phase === "success"
      ? "✓ Deploy successful"
      : "✗ Deploy failed";

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="max-w-lg" onKeyDown={(e) => e.key === "Escape" && phase !== "progress" && handleClose(false)}>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>

        {phase === "input" && (
          <form onSubmit={startDeploy} className="space-y-4">
            <Input
              label="Docker image"
              value={image}
              onChange={(e) => setImage(e.target.value)}
              placeholder="nginx:latest"
              required
            />
            {currentImage && (
              <p className="text-xs text-gray-500">Current version: {currentImage}</p>
            )}
            <button
              type="button"
              className="text-sm text-blue-600 hover:underline"
              onClick={() => setAdvanced((v) => !v)}
            >
              Advanced settings {advanced ? "▲" : "▼"}
            </button>
            {advanced && (
              <div className="space-y-3 rounded-lg border border-gray-200 p-3 dark:border-gray-700">
                <Input
                  label="Port override"
                  type="number"
                  value={port}
                  onChange={(e) => setPort(Number(e.target.value))}
                />
                <Input
                  label="Memory limit (MB)"
                  type="number"
                  value={memory || ""}
                  onChange={(e) => setMemory(Number(e.target.value) || 0)}
                  placeholder="Leave blank to keep current"
                />
              </div>
            )}
            <DialogFooter>
              <Button type="button" variant="secondary" onClick={() => handleClose(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={submitting || !image.trim()}>
                {submitting ? "Starting…" : "Deploy →"}
              </Button>
            </DialogFooter>
          </form>
        )}

        {phase === "progress" && (
          <div className="space-y-4">
            <ul className="space-y-2">
              {DEPLOY_STEPS.map((s, i) => {
                const done = completedSteps.has(i) || percent >= 100;
                const current = !done && i === activeStep;
                return (
                  <li key={s.key} className="flex items-center gap-2 text-sm">
                    {done ? (
                      <Check className="h-4 w-4 text-green-600" />
                    ) : current ? (
                      <Loader2 className="h-4 w-4 animate-spin text-blue-600" />
                    ) : (
                      <Circle className="h-4 w-4 text-gray-300" />
                    )}
                    <span className={done ? "text-green-700 dark:text-green-400" : current ? "text-gray-900 dark:text-white" : "text-gray-400"}>
                      {s.label}
                    </span>
                  </li>
                );
              })}
            </ul>
            <Progress value={percent} />
            <p className="text-xs text-gray-500">
              {percent < 100 ? `In progress · ${percent}%` : "Finishing…"}
            </p>
            <Link
              href={`/servers/${serverId}/apps/${appId}?tab=logs`}
              target="_blank"
              className="text-sm text-blue-600 hover:underline"
            >
              View Logs
            </Link>
          </div>
        )}

        {phase === "success" && (
          <div className="space-y-4">
            {(resultUrl || liveUrl) && (
              <div className="rounded-lg bg-green-50 p-4 dark:bg-green-950/40">
                <p className="text-sm text-gray-600 dark:text-gray-300">Live URL</p>
                <a
                  href={resultUrl || liveUrl || "#"}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="mt-1 inline-flex items-center gap-1 font-medium text-blue-600 hover:underline"
                >
                  {(resultUrl || liveUrl || "").replace(/^https?:\/\//, "")}
                  <ExternalLink className="h-3.5 w-3.5" />
                </a>
                <a
                  href={resultUrl || liveUrl || "#"}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="mt-2 inline-block"
                >
                  <Button size="sm">Open →</Button>
                </a>
              </div>
            )}
            <p className="text-sm text-gray-500">Took {durationSec} seconds</p>
            <DialogFooter>
              <Button onClick={() => handleClose(false)}>Done</Button>
            </DialogFooter>
          </div>
        )}

        {phase === "failure" && (
          <div className="space-y-4">
            <p className="text-sm text-gray-700 dark:text-gray-200">
              Your previous version is still running. Your site is up.
            </p>
            {errorLogs && (
              <pre className="max-h-48 overflow-auto rounded-lg bg-gray-900 p-3 font-mono text-xs text-gray-100">
                {errorLogs}
              </pre>
            )}
            <div className="rounded-lg border border-gray-200 p-3 text-sm text-gray-600 dark:border-gray-700 dark:text-gray-300">
              <p className="font-medium text-gray-800 dark:text-gray-100">Common causes</p>
              <ul className="mt-1 list-disc pl-5 space-y-0.5">
                <li>Incorrect environment variable</li>
                <li>Database not available yet</li>
                <li>App listening on wrong port</li>
              </ul>
            </div>
            <DialogFooter>
              <Link href={`/servers/${serverId}/apps/${appId}?tab=logs`}>
                <Button variant="secondary" onClick={() => handleClose(false)}>
                  View Full Logs
                </Button>
              </Link>
              <Button onClick={resetToInput}>Try Again</Button>
            </DialogFooter>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
