"use client";

import { Check, Circle, Loader2, XCircle } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";

import { Progress } from "@/components/ui/progress";
import {
  INFER_DEPLOY_STEPS,
  type InferDeployState,
  type InferProgress,
} from "@/hooks/use-infer";

// ---------------------------------------------------------------------------
// Step state derivation
// ---------------------------------------------------------------------------

function stepState(
  index: number,
  progress: InferProgress | null,
  phase: InferDeployState["phase"]
): "pending" | "active" | "done" | "failed" {
  if (phase === "failed") {
    return index === 0 ? "failed" : "pending";
  }
  if (!progress) return "pending";
  const currentIdx = INFER_DEPLOY_STEPS.findIndex((s) => s.key === progress.phase);
  const idx = currentIdx >= 0 ? currentIdx : Math.floor(progress.percent / 10);
  if (index < idx || progress.percent >= 100) return "done";
  if (index === idx) return "active";
  return "pending";
}

function StepIcon({ state }: { state: "pending" | "active" | "done" | "failed" }) {
  if (state === "done") return <Check className="h-4 w-4 text-[var(--color-success)]" />;
  if (state === "active")
    return <Loader2 className="h-4 w-4 animate-spin text-[var(--color-accent)]" />;
  if (state === "failed") return <XCircle className="h-4 w-4 text-[var(--color-danger)]" />;
  return <Circle className="h-4 w-4 text-[var(--color-muted)]/60" />;
}

function elapsedLabel(startedAt: number | null): string {
  if (!startedAt) return "";
  const s = Math.floor((Date.now() - startedAt) / 1000);
  if (s < 60) return `${s}s elapsed`;
  return `${Math.floor(s / 60)}m ${s % 60}s elapsed`;
}

interface DeployProgressProps {
  modelName: string;
  state: InferDeployState;
}

export function DeployProgress({ modelName, state }: DeployProgressProps) {
  const { progress, phase, startedAt } = state;
  const percent = progress?.percent ?? 5;
  const activeMessage =
    phase === "failed"
      ? state.error || "Deployment failed"
      : progress?.message || "Starting deployment…";

  return (
    <Card>
      <CardContent className="space-y-5">
        <div className="flex items-center justify-between gap-3">
          <h2 className="text-lg font-bold tracking-tight text-[var(--color-ink)]">
            Deploying {modelName}…
          </h2>
          <span className="text-xs tabular-nums text-[var(--color-muted)]">
            {elapsedLabel(startedAt)}
          </span>
        </div>

        <ul className="space-y-2">
          {INFER_DEPLOY_STEPS.map((step, i) => {
            const st = stepState(i, progress, phase);
            return (
              <li key={step.key} className="flex items-center gap-2.5 text-sm">
                <StepIcon state={st} />
                <span
                  className={
                    st === "done"
                      ? "text-[var(--color-muted)]"
                      : st === "active"
                        ? "font-semibold text-[var(--color-ink)]"
                        : st === "failed"
                          ? "font-semibold text-[var(--color-danger)]"
                          : "text-[var(--color-muted)]/70"
                  }
                >
                  {step.label}
                </span>
                {st === "active" && (
                  <span className="text-xs text-[var(--color-muted)]">
                    · {activeMessage}
                  </span>
                )}
              </li>
            );
          })}
        </ul>

        <div className="space-y-2">
          <Progress value={phase === "failed" ? 0 : percent} />
          <div className="flex items-center justify-between text-xs text-[var(--color-muted)]">
            <span>{phase === "failed" ? "Deployment failed" : activeMessage}</span>
            <span className="tabular-nums">
              {phase === "failed" ? "—" : `${Math.min(100, Math.round(percent))}%`}
            </span>
          </div>
          {phase !== "failed" && (
            <p className="text-xs text-[var(--color-muted)]">
              First deploy downloads the model (5–15 min). Future deployments skip the
              download and start in minutes.
            </p>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
