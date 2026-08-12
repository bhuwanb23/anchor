"use client";

import { ArrowDownRight, ArrowUpRight, Gauge, Minus } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import type { BenchmarkComparison } from "@/types";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function fmtPct(pct: number, higherIsBetter: boolean): string {
  if (Number.isNaN(pct)) return "—";
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
  if (typeof v !== "number" || Number.isNaN(v)) return "—";
  return `${v.toFixed(1)} tok/s`;
}

function fmtMs(v?: number): string {
  if (typeof v !== "number" || Number.isNaN(v)) return "—";
  return `${Math.round(v)} ms`;
}

interface BenchmarkCardProps {
  comparison: BenchmarkComparison;
  hardwareLabel: string;
  modelLabel: string;
}

export function BenchmarkCard({ comparison, hardwareLabel, modelLabel }: BenchmarkCardProps) {
  const opt = comparison.optimized;
  const gen = comparison.generic;
  const wow = comparison.tokens_per_second_improvement_pct;
  const wowLabel = Number.isFinite(wow) && wow >= 0 ? `${Math.round(wow)}% faster on Arm` : "Same on Arm";

  return (
    <Card className="overflow-hidden">
      <CardContent className="space-y-5">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2">
            <Gauge className="h-5 w-5 text-[var(--color-accent)]" />
            <h2 className="text-lg font-bold tracking-tight text-[var(--color-ink)]">
              Benchmark Results
            </h2>
            {opt?.performix && <Badge variant="info">Arm Performix</Badge>}
          </div>
        </div>

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
                <td className="px-4 py-3 text-[var(--color-ink)]">Time to first token</td>
                <td className="px-4 py-3 tabular-nums text-[var(--color-muted)]">
                  {fmtMs(gen?.median_time_to_first_token_ms)}
                </td>
                <td className="px-4 py-3 font-semibold tabular-nums text-[var(--color-accent)]">
                  {fmtMs(opt?.median_time_to_first_token_ms)}
                </td>
                <td className="flex items-center justify-end gap-1 px-4 py-3 font-bold tabular-nums text-[var(--color-ink)]">
                  {comparison.ttft_improvement_pct >= 0 ? (
                    <ArrowDownRight className="h-4 w-4 text-[var(--color-success)]" />
                  ) : (
                    <ArrowUpRight className="h-4 w-4 text-[var(--color-danger)]" />
                  )}
                  {fmtPct(comparison.ttft_improvement_pct, false)}
                </td>
              </tr>
              <tr>
                <td className="px-4 py-3 text-[var(--color-ink)]">Peak memory</td>
                <td className="px-4 py-3 tabular-nums text-[var(--color-muted)]">
                  {fmtBytes(gen?.peak_memory_bytes)}
                </td>
                <td className="px-4 py-3 tabular-nums text-[var(--color-muted)]">
                  {fmtBytes(opt?.peak_memory_bytes)}
                </td>
                <td className="flex items-center justify-end gap-1 px-4 py-3 font-medium tabular-nums text-[var(--color-muted)]">
                  <Minus className="h-4 w-4" /> Same
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
      </CardContent>
    </Card>
  );
}
