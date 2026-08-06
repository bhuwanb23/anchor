"use client";

import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";

interface Metrics {
  cpu_percent?: number;
  ram_used_mb?: number;
  ram_total_mb?: number;
  ram_percent?: number;
  disk_used_gb?: number;
  disk_total_gb?: number;
  load_1min?: number;
}

interface MetricsGaugesProps {
  metrics: Metrics | null;
}

function Gauge({ label, value, unit, max }: { label: string; value: number; unit: string; max: number }) {
  const pct = Math.min(100, (value / max) * 100);
  const color = pct > 90 ? "text-red-600" : pct > 70 ? "text-amber-600" : "text-green-600";
  return (
    <div className="space-y-1">
      <div className="flex justify-between text-sm">
        <span className="text-gray-500">{label}</span>
        <span className={`font-medium ${color}`}>
          {value}{unit}
        </span>
      </div>
      <div className="h-2 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-gray-800">
        <div
          className={`h-full rounded-full transition-all ${
            pct > 90 ? "bg-red-600" : pct > 70 ? "bg-amber-600" : "bg-green-600"
          }`}
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  );
}

export function MetricsGauges({ metrics }: MetricsGaugesProps) {
  if (!metrics) {
    return (
      <Card>
        <CardHeader><CardTitle className="text-sm">Metrics</CardTitle></CardHeader>
        <CardContent>
          <p className="text-sm text-gray-500">No metrics available. Waiting for agent report.</p>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader><CardTitle className="text-sm">System Metrics</CardTitle></CardHeader>
      <CardContent className="space-y-4">
        <Gauge label="CPU" value={metrics.cpu_percent || 0} unit="%" max={100} />
        <Gauge
          label="RAM"
          value={metrics.ram_used_mb || 0}
          unit={` / ${metrics.ram_total_mb || 0} MB`}
          max={metrics.ram_total_mb || 1}
        />
        <Gauge
          label="Disk"
          value={metrics.disk_used_gb || 0}
          unit={` / ${metrics.disk_total_gb || 0} GB`}
          max={metrics.disk_total_gb || 1}
        />
        {metrics.load_1min !== undefined && (
          <div className="flex justify-between text-sm">
            <span className="text-gray-500">Load (1m)</span>
            <span className="font-medium">{metrics.load_1min.toFixed(2)}</span>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
