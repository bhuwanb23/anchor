import { type HTMLAttributes } from "react";

interface BadgeProps extends HTMLAttributes<HTMLSpanElement> {
  variant?: "default" | "success" | "warning" | "danger" | "info";
}

const variantStyles = {
  default: "bg-[var(--color-paper-2)] text-[var(--color-muted)]",
  success: "bg-[var(--color-success-soft)] text-[var(--color-success)]",
  warning: "bg-[var(--color-warning-soft)] text-[var(--color-warning)]",
  danger: "bg-[var(--color-danger-soft)] text-[var(--color-danger)]",
  info: "bg-[var(--color-info-soft)] text-[var(--color-info)]",
};

function Badge({ variant = "default", className = "", children, ...props }: BadgeProps) {
  return (
    <span
      className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-semibold tracking-tight ${variantStyles[variant]} ${className}`}
      {...props}
    >
      {children}
    </span>
  );
}

function StatusBadge({ status }: { status?: string | null }) {
  const key = status || "disconnected";
  const variantMap: Record<string, "success" | "danger" | "warning" | "info" | "default"> = {
    connected: "success",
    disconnected: "default",
    pending: "warning",
    updating: "warning",
    error: "danger",
    success: "success",
    running: "success",
    stopped: "default",
    failed: "danger",
    crashed: "danger",
    deploying: "warning",
    rolled_back: "warning",
    partial: "warning",
  };

  const labels: Record<string, string> = {
    connected: "Connected",
    disconnected: "Disconnected",
    pending: "Pending",
    updating: "Updating",
    error: "Error",
    running: "Running",
    stopped: "Stopped",
    failed: "Crashed",
    crashed: "Crashed",
    deploying: "Deploying",
    success: "Success",
    rolled_back: "Rolled Back",
    partial: "Partial",
  };

  return (
    <Badge variant={variantMap[key] || "default"}>
      {labels[key] || key}
    </Badge>
  );
}

export { Badge, StatusBadge };
export type { BadgeProps };
