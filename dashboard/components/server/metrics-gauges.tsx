"use client";

import type { MetricsSnapshot } from "@/types";

interface MetricsGaugesProps {
  metrics: MetricsSnapshot | null;
}

function gaugeColor(pct: number, yellowAt: number, redAt: number): string {
  if (pct >= redAt) return "text-red-600 dark:text-red-400";
  if (pct >= yellowAt) return "text-amber-500 dark:text-amber-400";
  return "text-green-600 dark:text-green-400";
}

function barColor(pct: number, yellowAt: number, redAt: number): string {
  if (pct >= redAt) return "bg-red-500";
  if (pct >= yellowAt) return "bg-amber-400";
  return "bg-green-500";
}

function formatGB(n: number): string {
  if (n >= 10) return `${Math.round(n)}GB`;
  return `${n.toFixed(1)}GB`;
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
  const pct = Math.min(100, Math.max(0, percent));
  return (
    <div className="flex flex-col rounded-xl border border-gray-200 bg-white p-5 dark:border-gray-800 dark:bg-gray-900">
      <div className="mb-3 text-xs font-medium uppercase tracking-wider text-gray-400">
        {label}
      </div>
      <div className={`text-4xl font-semibold tabular-nums ${gaugeColor(pct, yellowAt, redAt)}`}>
        {Math.round(pct)}
        <span className="text-xl font-medium">%</span>
      </div>
      {subtitle && (
        <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">{subtitle}</p>
      )}
      <div className="mt-4 h-2 w-full overflow-hidden rounded-full bg-gray-100 dark:bg-gray-800">
        <div
          className={`h-full rounded-full transition-all duration-500 ${barColor(pct, yellowAt, redAt)}`}
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  );
}

export function MetricsGauges({ metrics }: MetricsGaugesProps) {
  if (!metrics) {
    return (
      <div className="grid gap-4 sm:grid-cols-3">
        {["CPU", "RAM", "Disk"].map((label) => (
          <div
            key={label}
            className="rounded-xl border border-dashed border-gray-200 p-5 dark:border-gray-800"
          >
            <div className="mb-3 text-xs font-medium uppercase tracking-wider text-gray-400">
              {label}
            </div>
            <p className="text-sm text-gray-400">Waiting for health report…</p>
          </div>
        ))}
      </div>
    );
  }

  const ramUsed = metrics.ram_used_mb / 1024;
  const ramTotal = metrics.ram_total_mb / 1024;

  return (
    <div className="grid gap-4 sm:grid-cols-3">
      <ResourceGauge
        label="CPU"
        percent={metrics.cpu_percent}
        yellowAt={70}
        redAt={85}
      />
      <ResourceGauge
        label="RAM"
        percent={metrics.ram_percent}
        subtitle={`${formatGB(ramUsed)} / ${formatGB(ramTotal)}`}
        yellowAt={70}
        redAt={85}
      />
      <ResourceGauge
        label="Disk"
        percent={metrics.disk_percent}
        subtitle={`${formatGB(metrics.disk_used_gb)} / ${formatGB(metrics.disk_total_gb)}`}
        yellowAt={75}
        redAt={90}
      />
    </div>
  );
}
