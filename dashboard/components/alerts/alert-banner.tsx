"use client";

import { AlertTriangle, X } from "lucide-react";
import { useState } from "react";

interface AlertBannerProps {
  message: string;
  severity?: "warning" | "error";
}

export function AlertBanner({ message, severity = "warning" }: AlertBannerProps) {
  const [dismissed, setDismissed] = useState(false);
  if (dismissed) return null;

  const bg = severity === "error" ? "bg-red-50 text-red-800 dark:bg-red-950 dark:text-red-300" : "bg-amber-50 text-amber-800 dark:bg-amber-950 dark:text-amber-300";

  return (
    <div className={`flex items-center gap-3 rounded-lg px-4 py-3 text-sm ${bg}`}>
      <AlertTriangle className="h-4 w-4 shrink-0" />
      <span className="flex-1">{message}</span>
      <button onClick={() => setDismissed(true)} className="shrink-0 opacity-70 hover:opacity-100">
        <X className="h-4 w-4" />
      </button>
    </div>
  );
}
