"use client";

import { useEffect, useRef, useState } from "react";
import {
  Check,
  Circle,
  Loader2,
  RotateCcw,
  Settings2,
  Terminal,
  XCircle,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import {
  INFER_DEPLOY_STEPS,
  INFER_BENCH_STEPS,
  stepIndexFor,
  type InferDeployState,
  type InferLogLine,
} from "@/hooks/use-infer";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function fmtDuration(ms: number): string {
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  const rest = s % 60;
  return rest === 0 ? `${m}m` : `${m}m ${rest}s`;
}

function fmtClock(ts: number): string {
  return new Date(ts).toLocaleTimeString([], {
    hour12: false,
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

// Rough ETA for the model download based on size parsed from the message
// ("Downloading model (4.7GB)...") at a nominal 20 MB/s.
function downloadEtaHint(message: string): string | null {
  const m = message.match(/\(([\d.]+)\s*GB\)/i);
  if (!m) return null;
  const gb = parseFloat(m[1]);
  if (!gb) return null;
  const seconds = Math.ceil((gb * 1024) / 20);
  return fmtDuration(seconds * 1000);
}

interface DeployProgressProps {
  modelName: string;
  state: InferDeployState;
  onRetry: () => void;
  onChangeConfig: () => void;
}

export function DeployProgress({ modelName, state, onRetry, onChangeConfig }: DeployProgressProps) {
  const { progress, phase, startedAt, log, stepTimings, stepIndex, mode, error } = state;

  const steps = mode === "benchmark" ? INFER_BENCH_STEPS : INFER_DEPLOY_STEPS;
  const percent = progress?.percent ?? (phase === "failed" ? 0 : 5);
  const now = Date.now();

  const [clock, setClock] = useState(now);
  useEffect(() => {
    const t = setInterval(() => setClock(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);

  // Auto-scroll the terminal to the bottom on new lines.
  const logRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = logRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [log.length]);

  const activeMessage =
    phase === "failed"
      ? error || "Command failed"
      : progress?.message || "Starting…";

  // Which step is highlighted as failed.
  const failedIndex =
    phase === "failed"
      ? Math.min(stepIndex, steps.length - 1)
      : -1;

  const totalElapsed = startedAt ? Math.max(0, clock - startedAt) : 0;
  const currentStepTiming = stepTimings[stepIndex];
  const currentStepElapsed = currentStepTiming
    ? Math.max(0, clock - currentStepTiming.started)
    : 0;

  const isDownloading =
    mode === "deploy" && stepIndex === 3 && phase !== "failed";
  const etaHint = isDownloading && progress ? downloadEtaHint(progress.message) : null;

  const title =
    mode === "benchmark"
      ? `Benchmarking ${modelName}…`
      : `Deploying ${modelName}…`;

  return (
    <Card className="border-[var(--color-accent-2)]/50">
      <CardContent className="space-y-5">
        {/* Title + elapsed */}
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 className="flex items-center gap-2 text-lg font-bold tracking-tight text-[var(--color-ink)]">
            {mode === "benchmark" ? (
              <Terminal className="h-5 w-5 text-[var(--color-accent)]" />
            ) : (
              <Loader2 className="h-5 w-5 animate-spin text-[var(--color-accent)]" />
            )}
            {title}
          </h2>
          <span className="text-xs tabular-nums text-[var(--color-muted)]">
            {totalElapsed > 0 ? `${fmtDuration(totalElapsed)} elapsed` : "starting…"}
          </span>
        </div>

        {/* Step list */}
        <ul className="space-y-1.5">
          {steps.map((step, i) => {
            const timing = stepTimings[i];
            const duration = timing?.done ? timing.done - timing.started : null;
            const st = failedIndex >= 0
              ? i === failedIndex
                ? "failed"
                : i < failedIndex
                  ? "done"
                  : "pending"
              : i < stepIndex
                ? "done"
                : i === stepIndex
                  ? "active"
                  : "pending";

            return (
              <li key={i} className="flex items-center gap-2.5 rounded-[var(--radius-sm)] px-2 py-1.5 text-sm transition-colors hover:bg-[var(--color-paper-2)]/60">
                {st === "done" && (
                  <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-[var(--color-success-soft)]">
                    <Check className="h-3 w-3 text-[var(--color-success)]" />
                  </span>
                )}
                {st === "active" && (
                  <Loader2 className="h-5 w-5 shrink-0 animate-spin text-[var(--color-accent)]" />
                )}
                {st === "failed" && (
                  <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-[var(--color-danger-soft)]">
                    <XCircle className="h-3.5 w-3.5 text-[var(--color-danger)]" />
                  </span>
                )}
                {st === "pending" && (
                  <Circle className="h-5 w-5 shrink-0 text-[var(--color-muted)]/50" />
                )}

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

                {st === "done" && duration !== null && (
                  <span className="text-xs tabular-nums text-[var(--color-muted)]/80">
                    ({fmtDuration(duration)})
                  </span>
                )}

                {st === "active" && (
                  <span className="truncate text-xs text-[var(--color-muted)]">
                    · {activeMessage}
                  </span>
                )}
              </li>
            );
          })}
        </ul>

        {/* One-time download note */}
        {isDownloading && (
          <div className="rounded-[var(--radius-md)] border border-[var(--color-warning)]/30 bg-[var(--color-warning-soft)] px-3.5 py-2.5 text-xs text-[var(--color-ink)]">
            <span className="font-semibold">First deploy:</span> downloading the model for the
            first time{etaHint ? ` — about ${etaHint} at current speed` : ""}. Future
            deployments skip this step.
          </div>
        )}

        {/* Progress bar + ETA */}
        <div className="space-y-2">
          <Progress value={phase === "failed" ? 0 : percent} />
          <div className="flex items-center justify-between text-xs text-[var(--color-muted)]">
            <span className="truncate pr-3">{activeMessage}</span>
            <span className="shrink-0 tabular-nums">
              {phase === "failed"
                ? "failed"
                : `${Math.min(100, Math.round(percent))}%`}
              {mode === "benchmark" && phase !== "failed" && currentStepElapsed > 0
                ? ` · step ${currentStepElapsed >= 1000 ? fmtDuration(currentStepElapsed) : "0s"}`
                : ""}
            </span>
          </div>
        </div>

        {/* Live log terminal */}
        <div className="overflow-hidden rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[#0b1020] shadow-inner">
          <div className="flex items-center gap-1.5 border-b border-white/10 px-3.5 py-2">
            <span className="h-2.5 w-2.5 rounded-full bg-[var(--color-danger)]/80" />
            <span className="h-2.5 w-2.5 rounded-full bg-[var(--color-warning)]/80" />
            <span className="h-2.5 w-2.5 rounded-full bg-[var(--color-success)]/80" />
            <span className="ml-2 flex items-center gap-1.5 font-mono text-[11px] text-white/50">
              <Terminal className="h-3 w-3" /> agent · deploy log
            </span>
          </div>
          <div
            ref={logRef}
            className="h-44 overflow-y-auto px-3.5 py-3 font-mono text-xs leading-relaxed text-[#c9d4e3]"
          >
            {log.length === 0 ? (
              <p className="text-white/30">Waiting for agent output…</p>
            ) : (
              log.map((line: InferLogLine, i: number) => (
                <p key={i} className="whitespace-pre-wrap break-words">
                  <span className="mr-2 select-none text-white/30">{fmtClock(line.at)}</span>
                  {line.text}
                </p>
              ))
            )}
          </div>
        </div>

        {/* Failed step handling */}
        {phase === "failed" && (
          <div className="space-y-3">
            <div className="rounded-[var(--radius-md)] border border-[var(--color-danger)]/30 bg-[var(--color-danger-soft)] px-4 py-3">
              <p className="text-sm font-semibold text-[var(--color-danger)]">
                Deployment failed
              </p>
              <p className="mt-1 text-sm text-[var(--color-ink)]">
                {error || "Something went wrong on the server. Check that Docker is running and try again."}
              </p>
            </div>
            <div className="flex flex-wrap gap-2">
              <Button size="sm" onClick={onRetry} className="gap-1.5">
                <RotateCcw className="h-3.5 w-3.5" /> Try Again
              </Button>
              <Button variant="secondary" size="sm" onClick={onChangeConfig} className="gap-1.5">
                <Settings2 className="h-3.5 w-3.5" /> Change Configuration
              </Button>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
