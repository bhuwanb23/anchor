import * as React from "react";

interface ProgressProps extends React.HTMLAttributes<HTMLDivElement> {
  value?: number;
  max?: number;
}

export function Progress({ value = 0, max = 100, className, ...props }: ProgressProps) {
  const v = typeof value === "number" && Number.isFinite(value) ? value : 0;
  const m = typeof max === "number" && Number.isFinite(max) && max > 0 ? max : 100;
  const pct = Math.min(100, Math.max(0, (v / m) * 100));
  return (
    <div
      className={`h-2.5 w-full overflow-hidden rounded-full bg-[var(--color-paper-2)] ${className || ""}`}
      {...props}
    >
      <div
        className="h-full rounded-full bg-[var(--color-accent)] transition-[width] duration-500 ease-[var(--ease-out)]"
        style={{ width: `${pct}%` }}
      />
    </div>
  );
}
