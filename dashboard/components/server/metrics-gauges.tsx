"use client";

import type { MetricsSnapshot } from "@/types";

interface MetricsGaugesProps {
  metrics: MetricsSnapshot | null | Partial<MetricsSnapshot>;
}

function num(n: unknown, fallback = 0): number {
  return typeof n === "number" && Number.isFinite(n) ? n : fallback;
}

function gaugeColor(pct: number, yellowAt: number, redAt: number): string {
  if (pct >= redAt) return "text-[var(--color-danger)]";
  if (pct >= yellowAt) return "text-[var(--color-warning)]";
  return "text-[var(--color-success)]";
}

function barColor(pct: number, yellowAt: number, redAt: number): string {
  if (pct >= redAt) return "bg-[var(--color-danger)]";
  if (pct >= yellowAt) return "bg-[var(--color-warning)]";
  return "bg-[var(--color-accent)]";
}

function formatGB(n: unknown): string {
  const v = num(n, 0);
  if (v >= 10) return `${Math.round(v)}GB`;
  return `${v.toFixed(1)}GB`;
}

function ResourceGauge({
  label,
  percent,
  subtitle,
  yellowAt,
  redAt,
}: {
  label: string;
  percent: number;
  subtitle?: string;
  yellowAt: number;
  redAt: number;
}) {
  const pct = Math.min(100, Math.max(0, num(percent)));
  return (
    <div className="flex flex-col rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] p-5 shadow-[var(--shadow-card)]">
      <div className="mb-3 text-xs font-semibold uppercase tracking-wider text-[var(--color-muted)]">
        {label}
      </div>
      <div className={`text-4xl font-extrabold tabular-nums tracking-tight ${gaugeColor(pct, yellowAt, redAt)}`}>
        {Math.round(pct)}
        <span className="text-xl font-semibold">%</span>
      </div>
      {subtitle && (
        <p className="mt-1 text-sm text-[var(--color-muted)]">{subtitle}</p>
      )}
      <div className="mt-4 h-2.5 w-full overflow-hidden rounded-full bg-[var(--color-paper-2)]">
        <div
          className={`h-full rounded-full transition-[width] duration-500 ease-[var(--ease-out)] ${barColor(pct, yellowAt, redAt)}`}
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  );
}

export function MetricsGauges({ metrics }: MetricsGaugesProps) {
  const hasAny =
    metrics &&
    (metrics.cpu_percent != null ||
      metrics.ram_percent != null ||
      metrics.disk_percent != null ||
      (metrics as { disk_used_percent?: number }).disk_used_percent != null);

  if (!hasAny) {
    return (
      <div className="grid gap-4 sm:grid-cols-3">
        {["CPU", "RAM", "Disk"].map((label) => (
          <div
            key={label}
            className="rounded-[var(--radius-lg)] border border-dashed border-[var(--color-border)] bg-[var(--color-surface)] p-5"
          >
            <div className="mb-3 text-xs font-semibold uppercase tracking-wider text-[var(--color-muted)]">
              {label}
            </div>
            <p className="text-sm text-[var(--color-muted)]">Waiting for health report…</p>
          </div>
        ))}
      </div>
    );
  }

  const cpu = num(metrics.cpu_percent);
  const ramPct = num(metrics.ram_percent);
  const ramUsed = num(metrics.ram_used_mb) / 1024;
  const ramTotal = num(metrics.ram_total_mb) / 1024;
  const diskPct = num(
    metrics.disk_percent ?? (metrics as { disk_used_percent?: number }).disk_used_percent
  );
  const diskUsed = num(metrics.disk_used_gb);
  const diskTotal = num(metrics.disk_total_gb);

  return (
    <div className="grid gap-4 sm:grid-cols-3">
      <ResourceGauge label="CPU" percent={cpu} yellowAt={70} redAt={85} />
      <ResourceGauge
        label="RAM"
        percent={ramPct}
        subtitle={`${formatGB(ramUsed)} / ${formatGB(ramTotal)}`}
        yellowAt={70}
        redAt={85}
      />
      <ResourceGauge
        label="Disk"
        percent={diskPct}
        subtitle={`${formatGB(diskUsed)} / ${formatGB(diskTotal)}`}
        yellowAt={75}
        redAt={90}
      />
    </div>
  );
}
