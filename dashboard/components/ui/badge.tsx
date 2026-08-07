import { type HTMLAttributes } from "react";

interface BadgeProps extends HTMLAttributes<HTMLSpanElement> {
  variant?: "default" | "success" | "warning" | "danger" | "info";
}

const variantStyles = {
  default: "bg-gray-100 text-gray-800 dark:bg-gray-800 dark:text-gray-200",
  success: "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200",
  warning: "bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200",
  danger: "bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200",
  info: "bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200",
};

function Badge({ variant = "default", className = "", children, ...props }: BadgeProps) {
  return (
    <span
      className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium ${variantStyles[variant]} ${className}`}
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
  };

  return (
    <Badge variant={variantMap[key] || "default"}>
      {labels[key] || key}
    </Badge>
  );
}

export { Badge, StatusBadge };
export type { BadgeProps };
