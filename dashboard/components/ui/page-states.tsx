"use client";

import Link from "next/link";
import { CloudOff, AlertTriangle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";

export function PageError({
  message = "We could not load this page. Try again in a moment.",
  requestId,
  onRetry,
}: {
  message?: string;
  requestId?: string | null;
  onRetry?: () => void;
}) {
  const rid = requestId || `req-${Date.now().toString(36)}`;
  return (
    <div className="mx-auto flex max-w-md flex-col items-center gap-4 py-16 text-center">
      <AlertTriangle className="h-10 w-10 text-amber-500" />
      <h2 className="text-xl font-semibold text-gray-900 dark:text-white">Something went wrong</h2>
      <p className="text-sm text-gray-600 dark:text-gray-300">{message}</p>
      <div className="flex flex-wrap items-center justify-center gap-3">
        {onRetry && (
          <Button onClick={onRetry}>Try again</Button>
        )}
        <Link href="/overview" className="text-sm text-blue-600 hover:underline">
          Go to dashboard
        </Link>
      </div>
      <p className="text-xs text-gray-400">
        If this keeps happening, contact support. Request ID: {rid}
      </p>
    </div>
  );
}

export function ServerDisconnectedBanner({ lastSeen }: { lastSeen?: string | null }) {
  const ago = lastSeen
    ? (() => {
        const mins = Math.floor((Date.now() - new Date(lastSeen).getTime()) / 60_000);
        if (mins < 1) return "just now";
        if (mins < 60) return `${mins} minute${mins === 1 ? "" : "s"} ago`;
        const hrs = Math.floor(mins / 60);
        return `${hrs} hour${hrs === 1 ? "" : "s"} ago`;
      })()
    : null;

  return (
    <div className="rounded-xl border border-gray-200 bg-gray-50 p-5 dark:border-gray-800 dark:bg-gray-900/50">
      <div className="flex gap-3">
        <CloudOff className="mt-0.5 h-5 w-5 shrink-0 text-gray-500" />
        <div className="space-y-1">
          <p className="font-medium text-gray-900 dark:text-white">
            Your server is not connected right now.
          </p>
          <p className="text-sm text-gray-600 dark:text-gray-300">
            Your apps are still running. This often happens during a brief network blip or agent restart.
          </p>
          <p className="text-sm text-gray-500">We will reconnect automatically.</p>
          {ago && (
            <p className="text-xs text-gray-400">Last connected {ago}</p>
          )}
        </div>
      </div>
    </div>
  );
}

export function ServerOverviewSkeleton() {
  return (
    <div className="mx-auto max-w-5xl space-y-8 animate-pulse">
      <div className="flex justify-between">
        <Skeleton className="h-9 w-48" />
        <Skeleton className="h-10 w-36 rounded-lg" />
      </div>
      <div className="flex justify-center gap-10">
        <Skeleton className="h-28 w-28 rounded-full" />
        <Skeleton className="h-28 w-28 rounded-full" />
        <Skeleton className="h-28 w-28 rounded-full" />
      </div>
      <div className="grid gap-4 sm:grid-cols-3">
        <Skeleton className="h-32 rounded-xl" />
        <Skeleton className="h-32 rounded-xl" />
        <Skeleton className="h-32 rounded-xl" />
      </div>
      <Skeleton className="h-4 w-full rounded" />
    </div>
  );
}

export function BackupsPageSkeleton() {
  return (
    <div className="space-y-6 animate-pulse">
      <Skeleton className="h-8 w-40" />
      <Skeleton className="h-20 w-full rounded-xl" />
      <Skeleton className="h-40 w-full rounded-xl" />
      <div className="space-y-2">
        <Skeleton className="h-16 w-full rounded-lg" />
        <Skeleton className="h-16 w-full rounded-lg" />
        <Skeleton className="h-16 w-full rounded-lg" />
      </div>
    </div>
  );
}

export function FadeIn({ children }: { children: React.ReactNode }) {
  return <div className="opacity-100 transition-opacity duration-150">{children}</div>;
}
