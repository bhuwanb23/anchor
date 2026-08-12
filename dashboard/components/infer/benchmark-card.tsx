"use client";

import { useState } from "react";
import {
  ArrowDownRight,
  ArrowUpRight,
  ChevronDown,
  Gauge,
  Loader2,
  Minus,
  RefreshCw,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import type { BenchmarkComparison } from "@/types";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function fmtPct(pct: number, higherIsBetter: boolean): string {
  if (!Number.isFinite(pct)) return "—";
  const abs = Math.abs(pct).toFixed(1);
  if (higherIsBetter) return pct >= 0 ? `+${abs}%` : `${abs}%`;
  // TTFT — positive improvement means faster
  return pct >= 0 ? `${abs}% faster` : `${abs}% slower`;
}

function fmtBytes(bytes?: number): string {
  if (!bytes) return "—";
  const gb = bytes / (1024 * 1024 * 1024);
  if (gb >= 1) return `${gb.toFixed(2)} GB`;
  const mb = bytes / (1024 * 1024);
  return `${mb.toFixed(0)} MB`;
}

function fmtTokSec(v?: number): string {
  if (typeof v !== "number" || !Number.isFinite(v)) return "—";
  return `${v.toFixed(1)} tok/s`;
}

function fmtMs(v?: number): string {
  if (typeof v !== "number" || !Number.isFinite(v)) return "—";
  return `${Math.round(v)} ms`;
}

function fmtBenchmarkTime(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleString([], {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function memoryDelta(genBytes?: number, optBytes?: number): string {
  if (!genBytes || !optBytes) return "Same";
  const diff = Math.abs(optBytes - genBytes) / (1024 * 1024 * 1024);
  if (diff < 0.05) return "Same";
  return optBytes < genBytes ? `${diff.toFixed(1)} GB lower` : `${diff.toFixed(1)} GB higher`;
}

interface BenchmarkCardProps {
  comparison: BenchmarkComparison;
  hardwareLabel: string;
  modelLabel: string;
  running: boolean;
  onRunAgain: () => void;
}

export function BenchmarkCard({
  comparison,
  hardwareLabel,
  modelLabel,
  running,
  onRunAgain,
}: BenchmarkCardProps) {
  const [showMethodology, setShowMethodology] = useState(false);
  const opt = comparison.optimized;
  const gen = comparison.generic;
  const wow = comparison.tokens_per_second_improvement_pct;
  const wowLabel =
    Number.isFinite(wow) && wow >= 0
      ? `${Math.round(wow)}% faster on Arm`
      : Number.isFinite(wow)
        ? `${Math.round(Math.abs(wow))}% slower on Arm`
        : "Same on Arm";

  const ttftLabel = (() => {
    const v = comparison.ttft_improvement_pct;
    if (!Number.isFinite(v)) return "—";
    return v >= 0 ? `${Math.round(v)}% lower` : `${Math.round(Math.abs(v))}% higher`;
  })();

  const promptCount = opt?.prompts?.length ?? 0;
  const methodology = [
    `Model: ${modelLabel || "Llama 3.1 8B Q4_K_M"} on ${hardwareLabel}.`,
    `Each build received ${promptCount > 0 ? promptCount : "the same"} prompts in the same order over an OpenAI-compatible API.`,
    `A warmup prompt was discarded; ${opt?.actual_runs ?? 2} measured runs were averaged.`,
    `Tokens per second = total tokens ÷ generation time; time to first token measured from request to first response token.`,
    `Memory usage captured from Docker stats during the run.`,
  ];

  return (
    <Card className="overflow-hidden">
      <CardContent className="space-y-5">
        {/* Title bar */}
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2">
            <Gauge className="h-5 w-5 text-[var(--color-accent)]" />
            <h2 className="text-lg font-bold tracking-tight text-[var(--color-ink)]">
              Benchmark Results
            </h2>
            {opt?.performix && <Badge variant="info">Arm Performix</Badge>}
          </div>
          {comparison.benchmarked_at && (
            <span className="text-xs tabular-nums text-[var(--color-muted)]">
              {fmtBenchmarkTime(comparison.benchmarked_at)}
            </span>
          )}
        </div>

        {/* Hardware context */}
        <p className="text-xs text-[var(--color-muted)]">
          Measured on {hardwareLabel}
          {modelLabel ? ` · Model: ${modelLabel}` : ""}
        </p>

        {/* Comparison table */}
        <div className="overflow-hidden rounded-[var(--radius-md)] border border-[var(--color-border)]">
          <table className="w-full text-sm">
            <thead>
              <tr className="bg-[var(--color-paper-2)] text-left text-xs font-semibold uppercase tracking-wider text-[var(--color-muted)]">
                <th className="px-4 py-2.5 font-semibold">Metric</th>
                <th className="px-4 py-2.5 font-semibold">Generic build</th>
                <th className="px-4 py-2.5 font-semibold text-[var(--color-accent)]">
                  Arm optimized
                </th>
                <th className="px-4 py-2.5 text-right font-semibold">Improvement</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-[var(--color-border)]">
              <tr>
                <td className="px-4 py-3 text-[var(--color-ink)]">Generation speed</td>
                <td className="px-4 py-3 tabular-nums text-[var(--color-muted)]">
                  {fmtTokSec(gen?.median_tokens_per_second)}
                </td>
                <td className="px-4 py-3 font-semibold tabular-nums text-[var(--color-accent)]">
                  {fmtTokSec(opt?.median_tokens_per_second)}
                </td>
                <td className="flex items-center justify-end gap-1 px-4 py-3 font-bold tabular-nums text-[var(--color-ink)]">
                  {comparison.tokens_per_second_improvement_pct >= 0 ? (
                    <ArrowUpRight className="h-4 w-4 text-[var(--color-success)]" />
                  ) : (
                    <ArrowDownRight className="h-4 w-4 text-[var(--color-danger)]" />
                  )}
                  {fmtPct(comparison.tokens_per_second_improvement_pct, true)}
                </td>
              </tr>
              <tr>
                <td className="px-4 py-3 text-[var(--color-ink)]">Time to 1st token</td>
                <td className="px-4 py-3 tabular-nums text-[var(--color-muted)]">
                  {fmtMs(gen?.median_ttft_ms)}
                </td>
                <td className="px-4 py-3 font-semibold tabular-nums text-[var(--color-accent)]">
                  {fmtMs(opt?.median_ttft_ms)}
                </td>
                <td className="flex items-center justify-end gap-1 px-4 py-3 font-bold tabular-nums text-[var(--color-ink)]">
                  {comparison.ttft_improvement_pct >= 0 ? (
                    <ArrowDownRight className="h-4 w-4 text-[var(--color-success)]" />
                  ) : (
                    <ArrowUpRight className="h-4 w-4 text-[var(--color-danger)]" />
                  )}
                  {ttftLabel}
                </td>
              </tr>
              <tr>
                <td className="px-4 py-3 text-[var(--color-ink)]">Memory usage</td>
                <td className="px-4 py-3 tabular-nums text-[var(--color-muted)]">
                  {fmtBytes(gen?.peak_memory_bytes)}
                </td>
                <td className="px-4 py-3 tabular-nums text-[var(--color-muted)]">
                  {fmtBytes(opt?.peak_memory_bytes)}
                </td>
                <td className="flex items-center justify-end gap-1 px-4 py-3 font-medium tabular-nums text-[var(--color-muted)]">
                  <Minus className="h-4 w-4" /> {memoryDelta(gen?.peak_memory_bytes, opt?.peak_memory_bytes)}
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        {/* WOW number */}
        <div className="rounded-[var(--radius-lg)] bg-[var(--color-accent-soft)] px-6 py-5 text-center">
          <p className="text-4xl font-extrabold tracking-tight text-[var(--color-accent)] sm:text-5xl">
            {wowLabel}
          </p>
          <p className="mt-1 text-sm text-[var(--color-muted)]">
            Same hardware · same model · different binary
          </p>
        </div>

        {/* Methodology (collapsed by default) */}
        <div className="overflow-hidden rounded-[var(--radius-md)] border border-[var(--color-border)]">
          <button
            type="button"
            onClick={() => setShowMethodology((v) => !v)}
            className="flex w-full items-center justify-between bg-[var(--color-paper-2)] px-4 py-2.5 text-sm font-semibold text-[var(--color-ink)] transition-colors hover:bg-[var(--color-paper-2)]/70"
          >
            <span>How is this measured?</span>
            <ChevronDown
              className={`h-4 w-4 text-[var(--color-muted)] transition-transform duration-[var(--dur-med)] ${
                showMethodology ? "rotate-180" : ""
              }`}
            />
          </button>
          {showMethodology && (
            <ul className="space-y-1.5 px-4 py-3 text-sm text-[var(--color-muted)]">
              {methodology.map((line, i) => (
                <li key={i} className="flex gap-2">
                  <span className="mt-1.5 h-1 w-1 shrink-0 rounded-full bg-[var(--color-accent)]" />
                  {line}
                </li>
              ))}
            </ul>
          )}
        </div>

        {/* Run benchmark again */}
        <div className="flex justify-end border-t border-[var(--color-border)] pt-4">
          <Button variant="secondary" size="sm" onClick={onRunAgain} disabled={running} className="gap-1.5">
            {running ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <RefreshCw className="h-3.5 w-3.5" />
            )}
            {running ? "Benchmarking…" : "Run benchmark again"}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
